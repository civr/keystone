package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/rbac"
)

// Accounts (Settings › Users): admins add people who sign in with a Keystone
// password. The owner is listed but managed in setup and Settings.

const (
	minPassword = 12
	maxName     = 100
)

type userView struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`  // owner, admin (any role) or member
	Roles       []string  `json:"roles"` // global role ids given directly
	Groups      []string  `json:"groups"`
	Disabled    bool      `json:"disabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func viewOf(a accounts.Account, roles []string) userView {
	role := "member"
	switch {
	case a.IsOwner():
		role, roles = "owner", []string{"superadmin"}
	case len(roles) > 0:
		role = "admin"
	}
	if roles == nil {
		roles = []string{}
	}
	return userView{ID: a.ID, Username: a.Username, DisplayName: a.DisplayName, Email: a.Email, Role: role, Roles: roles,
		Groups: a.Groups, Disabled: a.Disabled, CreatedAt: a.CreatedAt}
}

// directRoles maps account ids to the global roles given to them directly.
func (s *server) directRoles(ctx context.Context) (map[string][]string, error) {
	rows, err := s.d.Pool.Query(ctx, `select account_id, role_id from role_assignments
		where account_id is not null and app_id is null order by role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var acct, role string
		if err := rows.Scan(&acct, &role); err != nil {
			return nil, err
		}
		out[acct] = append(out[acct], role)
	}
	return out, rows.Err()
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	list, err := accounts.List(r.Context(), s.d.Pool)
	if err != nil {
		errInternal(w, "list users", err)
		return
	}
	roles, err := s.directRoles(r.Context())
	if err != nil {
		errInternal(w, "list roles", err)
		return
	}
	out := make([]userView, 0, len(list))
	for _, a := range list {
		out = append(out, viewOf(a, roles[a.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// legacyRoles turns the simple role choice (admin or member) into global
// roles, keeping the account's app-scoped ones.
func (s *server) legacyRoles(ctx context.Context, id, role string) ([]assignment, error) {
	cur, err := s.assignmentsOf(ctx, "account_id", id)
	if err != nil {
		return nil, err
	}
	var next []assignment
	for _, a := range cur {
		if a.App != "" || role == accounts.Admin {
			next = append(next, a)
		}
	}
	if role == accounts.Admin && !slices.ContainsFunc(next, func(a assignment) bool { return a.Role == "admin" && a.App == "" }) {
		next = append(next, assignment{Role: "admin"})
	}
	return next, nil
}

// mayManage reports whether the actor may change account target: only with
// at least every permission target has, so a password reset or a disabled
// account can never take over someone with more rights. False when an answer
// was written.
func (s *server) mayManage(w http.ResponseWriter, r *http.Request, target string) bool {
	ctx := r.Context()
	g, err := s.grants(ctx)
	if err != nil {
		errInternal(w, "load grants", err)
		return false
	}
	tg, err := rbac.Load(ctx, s.d.Pool, target)
	if err != nil {
		errInternal(w, "load grants", err)
		return false
	}
	if !g.Covers(tg.List()) || !g.CoversApps(tg) {
		writeError(w, http.StatusForbidden, "escalation", "This person has rights you do not have; you cannot change their account.")
		return false
	}
	return true
}

// passwordError is the message for an unacceptable new password, or "".
func passwordError(pw string) string {
	if utf8.RuneCountInString(pw) < minPassword {
		return "Choose a password of at least 12 characters."
	}
	if len(pw) > maxVaultField {
		return "The password may be at most 1024 characters."
	}
	return ""
}

// profileError checks a display name, e-mail address and role; field and
// message, or "" when fine.
func profileError(display, email, role string) (string, string) {
	switch {
	case utf8.RuneCountInString(display) > maxName:
		return "display_name", "The name may be at most 100 characters."
	case email != "" && !validEmail(email):
		return "email", "Enter a valid e-mail address."
	case role != accounts.Admin && role != accounts.Member:
		return "role", "Choose admin or member."
	}
	return "", ""
}

func validEmail(e string) bool {
	a, err := mail.ParseAddress(e)
	return err == nil && a.Address == e
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		Role        string `json:"role"` // admin or member; roles are set under /roles
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.DisplayName, in.Email = strings.TrimSpace(in.DisplayName), strings.TrimSpace(in.Email)
	if !accounts.ValidUsername(in.Username) {
		writeInvalid(w, "username", "Use lowercase letters, digits, dot, dash or underscore (at most 64).")
		return
	}
	if in.Role == "" {
		in.Role = accounts.Member
	}
	if f, msg := profileError(in.DisplayName, in.Email, in.Role); f != "" {
		writeInvalid(w, f, msg)
		return
	}
	if msg := passwordError(in.Password); msg != "" {
		writeInvalid(w, "password", msg)
		return
	}
	if !s.emailFree(ctx, w, in.Email, "") {
		return
	}
	if taken, err := s.ownerAppName(ctx, in.Username); err != nil || taken {
		if err != nil {
			errInternal(w, "load apps", err)
		} else {
			writeInvalid(w, "username", "The owner uses this name at one of the apps. Choose another.")
		}
		return
	}
	if in.Role == accounts.Admin {
		if g, err := s.grants(ctx); err != nil || !g.Has(rbac.RolesWrite) || !g.Covers(rbac.AdminPermissions) {
			writeError(w, http.StatusForbidden, "escalation", "You cannot make someone an administrator.")
			return
		}
	}
	hash, err := crypto.HashSecret(in.Password)
	if err != nil {
		errInternal(w, "hash password", err)
		return
	}
	a, err := accounts.Create(ctx, s.d.Pool, accounts.New{Username: in.Username, DisplayName: in.DisplayName, Email: in.Email, PasswordHash: hash, Role: accounts.Member})
	if errors.Is(err, accounts.ErrTaken) {
		writeInvalid(w, "username", "This username is already in use.")
		return
	}
	if err != nil {
		errInternal(w, "create user", err)
		return
	}
	var roles []string
	if in.Role == accounts.Admin {
		if _, err := s.d.Pool.Exec(ctx, `insert into role_assignments (role_id, account_id) values ('admin', $1)`, a.ID); err != nil {
			errInternal(w, "assign role", err)
			return
		}
		roles = []string{"admin"}
	}
	s.d.Audit.Log(ctx, actor(ctx), "user.created", clientIP(ctx), map[string]any{"id": a.ID, "username": a.Username, "roles": roles})
	writeJSON(w, http.StatusCreated, viewOf(a, roles))
}

func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == accounts.OwnerID {
		writeError(w, http.StatusForbidden, "owner", "The owner is changed under Settings › General.")
		return
	}
	if _, ok := appID(w, r); !ok {
		return
	}
	var in struct {
		DisplayName *string `json:"display_name"`
		Email       *string `json:"email"`
		Role        *string `json:"role"`
		Disabled    *bool   `json:"disabled"`
		Password    *string `json:"password"` // set: an admin's reset
	}
	if !readJSON(w, r, &in) {
		return
	}
	cur, err := accounts.Load(ctx, s.d.Pool, id)
	if errors.Is(err, accounts.ErrNotFound) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "load user", err)
		return
	}
	if !s.mayManage(w, r, id) {
		return
	}
	display, email, role := cur.DisplayName, cur.Email, accounts.Member
	if in.DisplayName != nil {
		v := strings.TrimSpace(*in.DisplayName)
		in.DisplayName, display = &v, v
	}
	if in.Email != nil {
		v := strings.TrimSpace(*in.Email)
		in.Email, email = &v, v
	}
	if in.Role != nil {
		role = *in.Role
	}
	if f, msg := profileError(display, email, role); f != "" {
		writeInvalid(w, f, msg)
		return
	}
	if in.Email != nil && !s.emailFree(ctx, w, email, id) {
		return
	}
	if in.Role != nil {
		next, err := s.legacyRoles(ctx, id, *in.Role)
		if err != nil {
			errInternal(w, "load roles", err)
			return
		}
		if g, err := s.grants(ctx); err != nil || !g.Has(rbac.RolesWrite) {
			writeError(w, http.StatusForbidden, "forbidden", "You cannot change roles.")
			return
		}
		if !s.replaceAssignments(w, r, "account_id", id, next) {
			return
		}
	}
	change := accounts.Change{DisplayName: in.DisplayName, Email: in.Email, Disabled: in.Disabled}
	if in.Password != nil {
		if msg := passwordError(*in.Password); msg != "" {
			writeInvalid(w, "password", msg)
			return
		}
		hash, err := crypto.HashSecret(*in.Password)
		if err != nil {
			errInternal(w, "hash password", err)
			return
		}
		change.PasswordHash = &hash
	}
	a, err := accounts.Update(ctx, s.d.Pool, id, change)
	if err != nil {
		errInternal(w, "update user", err)
		return
	}
	// A new password or a disabled account signs out everywhere.
	if in.Password != nil || a.Disabled {
		if err := s.d.Sessions.RevokeUser(ctx, a.ID); err != nil {
			slog.Error("users: revoke sessions", "err", err)
		}
	}
	roles, err := s.directRoles(ctx)
	if err != nil {
		errInternal(w, "list roles", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "user.updated", clientIP(ctx), map[string]any{
		"id": a.ID, "username": a.Username, "roles": roles[a.ID], "disabled": a.Disabled, "password_reset": in.Password != nil,
	})
	writeJSON(w, http.StatusOK, viewOf(a, roles[a.ID]))
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == accounts.OwnerID {
		writeError(w, http.StatusForbidden, "owner", "The owner cannot be deleted.")
		return
	}
	if _, ok := appID(w, r); !ok {
		return
	}
	if id == SessionFrom(ctx).UserID {
		writeError(w, http.StatusConflict, "self", "You cannot delete your own account.")
		return
	}
	if !s.mayManage(w, r, id) {
		return
	}
	a, err := accounts.Load(ctx, s.d.Pool, id)
	if errors.Is(err, accounts.ErrNotFound) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "load user", err)
		return
	}
	if err := accounts.Delete(ctx, s.d.Pool, id); err != nil {
		errInternal(w, "delete user", err)
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `update apps set access = array_remove(access, $1) where $1 = any(access)`, id); err != nil {
		slog.Error("users: drop from app access", "err", err)
	}
	if err := s.d.Sessions.RevokeUser(ctx, id); err != nil {
		slog.Error("users: revoke sessions", "err", err)
	}
	s.d.Audit.Log(ctx, actor(ctx), "user.deleted", clientIP(ctx), map[string]any{"id": id, "username": a.Username})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// emailFree checks that email is no other account's, nor the owner's address
// at an app (an override): apps that link people by e-mail would merge them.
// False when an answer was written.
func (s *server) emailFree(ctx context.Context, w http.ResponseWriter, email, except string) bool {
	if email == "" {
		return true
	}
	taken, err := accounts.EmailTaken(ctx, s.d.Pool, email, except)
	if err == nil && !taken {
		var list []apps.App
		if list, err = apps.Load(ctx, s.d.Pool); err == nil {
			for _, a := range list {
				taken = taken || strings.EqualFold(a.Config.Account.Email, email)
			}
		}
	}
	if err != nil {
		errInternal(w, "check e-mail", err)
		return false
	}
	if taken {
		writeInvalid(w, "email", "Another person already uses this e-mail address.")
		return false
	}
	return true
}

// ownerAppName reports whether username is the name the owner is given at
// some app (an account override). A user with that name would be the owner
// there, so such names are refused.
func (s *server) ownerAppName(ctx context.Context, username string) (bool, error) {
	list, err := apps.Load(ctx, s.d.Pool)
	if err != nil {
		return false, err
	}
	for _, a := range list {
		if strings.EqualFold(a.Config.Account.Username, username) {
			return true, nil
		}
	}
	return false, nil
}

// changeOwnPassword sets the signed-in account's password after checking the
// current one. An owner who signs in with AD changes it in AD.
func (s *server) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	acct, err := s.account(ctx)
	if err != nil {
		errInternal(w, "load account", err)
		return
	}
	if acct.PasswordHash == "" {
		writeError(w, http.StatusConflict, "ad_password", "You sign in with your Active Directory password; change it there.")
		return
	}
	if msg := passwordError(in.New); msg != "" {
		writeInvalid(w, "new", msg)
		return
	}
	if !s.allow(w, ctx, "login-account", acct.Username, cfg.BruteForce.LoginPerAccountPerMinute) ||
		!s.notLocked(w, ctx, acct.Username, ip) {
		return
	}
	if !crypto.VerifySecret(acct.PasswordHash, in.Current) {
		// Counts toward the lockout like a failed sign-in.
		if _, err := s.d.Limiter.Fail(ctx, acct.Username, cfg); err != nil {
			slog.Error("account: record failure", "err", err)
		}
		writeInvalid(w, "current", "That is not your current password.")
		return
	}
	hash, err := crypto.HashSecret(in.New)
	if err != nil {
		errInternal(w, "hash password", err)
		return
	}
	if err := accounts.SetPassword(ctx, s.d.Pool, acct.ID, hash); err != nil {
		errInternal(w, "set password", err)
		return
	}
	// Other sessions of this account end; this one stays.
	list, err := s.d.Sessions.List(ctx)
	if err == nil {
		for _, x := range list {
			if x.UserID == SessionFrom(ctx).UserID && x.ID != SessionFrom(ctx).ID {
				_ = s.d.Sessions.Revoke(ctx, x.ID)
			}
		}
	}
	s.d.Audit.Log(ctx, actor(ctx), "account.password_changed", ip, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
