package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/rbac"
)

// Roles, groups and who has which role (package rbac). The rules that keep
// this safe: nobody hands out or takes away a role whose permissions they do
// not hold themselves, only a super admin makes super admins, built-in roles
// cannot be edited, the owner's role cannot be changed, and joining a group
// that carries roles counts as being given those roles.

type roleView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Builtin     bool     `json:"builtin"`
}

func (s *server) loadRoles(ctx context.Context) (map[string]roleView, []roleView, error) {
	rows, err := s.d.Pool.Query(ctx, `select id, name, description, permissions, builtin from roles order by builtin desc, name`)
	if err != nil {
		return nil, nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (roleView, error) {
		var v roleView
		return v, r.Scan(&v.ID, &v.Name, &v.Description, &v.Permissions, &v.Builtin)
	})
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]roleView{}
	for _, v := range list {
		byID[v.ID] = v
	}
	return byID, list, nil
}

func (s *server) listRoles(w http.ResponseWriter, r *http.Request) {
	_, list, err := s.loadRoles(r.Context())
	if err != nil {
		errInternal(w, "list roles", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": list, "permissions": rbac.All})
}

var roleNameRe = regexp.MustCompile(`^[\p{L}\p{N} ._-]{1,60}$`)

// readRole decodes and checks a custom role; the actor must hold every
// permission in it.
func (s *server) readRole(w http.ResponseWriter, r *http.Request) (roleView, bool) {
	var in roleView
	if !readJSON(w, r, &in) {
		return in, false
	}
	in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if !roleNameRe.MatchString(in.Name) {
		writeInvalid(w, "name", "Give the role a name of at most 60 characters.")
		return in, false
	}
	if utf8.RuneCountInString(in.Description) > 200 {
		writeInvalid(w, "description", "At most 200 characters.")
		return in, false
	}
	slices.Sort(in.Permissions)
	in.Permissions = slices.Compact(in.Permissions)
	for _, p := range in.Permissions {
		if !rbac.Known(p) || p == rbac.Everything {
			writeInvalid(w, "permissions", "Unknown permission "+p+".")
			return in, false
		}
	}
	if g, err := s.grants(r.Context()); err != nil || !g.Covers(in.Permissions) {
		writeError(w, http.StatusForbidden, "escalation", "You cannot give a role permissions you do not have.")
		return in, false
	}
	return in, true
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func (s *server) createRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, ok := s.readRole(w, r)
	if !ok {
		return
	}
	in.ID = uuid.NewString()
	_, err := s.d.Pool.Exec(ctx, `insert into roles (id, name, description, permissions) values ($1, $2, $3, $4)`,
		in.ID, in.Name, in.Description, in.Permissions)
	if isUnique(err) {
		writeInvalid(w, "name", "A role with this name exists.")
		return
	}
	if err != nil {
		errInternal(w, "create role", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "role.created", clientIP(ctx), map[string]any{"id": in.ID, "name": in.Name, "permissions": in.Permissions})
	writeJSON(w, http.StatusCreated, in)
}

func (s *server) updateRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	byID, _, err := s.loadRoles(ctx)
	if err != nil {
		errInternal(w, "load roles", err)
		return
	}
	cur, ok := byID[id]
	if !ok {
		errNotFound(w, r)
		return
	}
	if cur.Builtin {
		writeError(w, http.StatusConflict, "builtin", "Built-in roles cannot be changed. Make a new role instead.")
		return
	}
	in, ok := s.readRole(w, r)
	if !ok {
		return
	}
	// Taking permissions away from a role also needs them.
	if g, err := s.grants(ctx); err != nil || !g.Covers(cur.Permissions) {
		writeError(w, http.StatusForbidden, "escalation", "You cannot change a role with permissions you do not have.")
		return
	}
	_, err = s.d.Pool.Exec(ctx, `update roles set name = $2, description = $3, permissions = $4 where id = $1`, id, in.Name, in.Description, in.Permissions)
	if isUnique(err) {
		writeInvalid(w, "name", "A role with this name exists.")
		return
	}
	if err != nil {
		errInternal(w, "update role", err)
		return
	}
	in.ID = id
	s.d.Audit.Log(ctx, actor(ctx), "role.updated", clientIP(ctx), map[string]any{"id": id, "name": in.Name, "permissions": in.Permissions})
	writeJSON(w, http.StatusOK, in)
}

func (s *server) deleteRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	byID, _, err := s.loadRoles(ctx)
	if err != nil {
		errInternal(w, "load roles", err)
		return
	}
	cur, ok := byID[id]
	if !ok {
		errNotFound(w, r)
		return
	}
	if cur.Builtin {
		writeError(w, http.StatusConflict, "builtin", "Built-in roles cannot be deleted.")
		return
	}
	if g, err := s.grants(ctx); err != nil || !g.Covers(cur.Permissions) {
		writeError(w, http.StatusForbidden, "escalation", "You cannot delete a role with permissions you do not have.")
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `delete from roles where id = $1`, id); err != nil {
		errInternal(w, "delete role", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "role.deleted", clientIP(ctx), map[string]any{"id": id, "name": cur.Name})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// assignment is one role, globally (App "") or for one app.
type assignment struct {
	Role string `json:"role"`
	App  string `json:"app,omitempty"`
}

// assignmentsOf lists the roles given directly to an account or a group.
func (s *server) assignmentsOf(ctx context.Context, column, id string) ([]assignment, error) {
	rows, err := s.d.Pool.Query(ctx, `select role_id, coalesce(app_id::text, '') from role_assignments where `+column+` = $1 order by role_id, app_id`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (assignment, error) {
		var a assignment
		return a, r.Scan(&a.Role, &a.App)
	})
}

// replaceAssignments sets the roles of an account or group to next. Every
// role added or removed must be within the actor's permissions; app-scoped
// roles must name an existing app.
func (s *server) replaceAssignments(w http.ResponseWriter, r *http.Request, column, id string, next []assignment) bool {
	ctx := r.Context()
	byID, _, err := s.loadRoles(ctx)
	if err != nil {
		errInternal(w, "load roles", err)
		return false
	}
	cur, err := s.assignmentsOf(ctx, column, id)
	if err != nil {
		errInternal(w, "load assignments", err)
		return false
	}
	g, err := s.grants(ctx)
	if err != nil {
		errInternal(w, "load grants", err)
		return false
	}
	key := func(a assignment) string { return a.Role + "@" + a.App }
	want := map[string]assignment{}
	for _, a := range next {
		if _, ok := byID[a.Role]; !ok {
			writeInvalid(w, "roles", "Unknown role "+a.Role+".")
			return false
		}
		if a.App != "" {
			if _, err := uuid.Parse(a.App); err != nil {
				writeInvalid(w, "roles", "Unknown app.")
				return false
			}
		}
		want[key(a)] = a
	}
	have := map[string]assignment{}
	for _, a := range cur {
		have[key(a)] = a
	}
	var changed []assignment
	for k, a := range want {
		if _, ok := have[k]; !ok {
			changed = append(changed, a)
		}
	}
	for k, a := range have {
		if _, ok := want[k]; !ok {
			changed = append(changed, a)
		}
	}
	for _, a := range changed {
		if !g.Covers(byID[a.Role].Permissions) {
			writeError(w, http.StatusForbidden, "escalation", "You cannot give or take away "+byID[a.Role].Name+": it has permissions you do not have.")
			return false
		}
	}
	tx, err := s.d.Pool.Begin(ctx)
	if err != nil {
		errInternal(w, "assign roles", err)
		return false
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `delete from role_assignments where `+column+` = $1`, id); err != nil {
		errInternal(w, "assign roles", err)
		return false
	}
	for _, a := range want {
		var app *string
		if a.App != "" {
			app = &a.App
		}
		var err error
		if column == "account_id" {
			_, err = tx.Exec(ctx, `insert into role_assignments (role_id, account_id, app_id) values ($1, $2, $3)`, a.Role, id, app)
		} else {
			_, err = tx.Exec(ctx, `insert into role_assignments (role_id, group_id, app_id) values ($1, $2, $3)`, a.Role, id, app)
		}
		if err != nil {
			writeInvalid(w, "roles", "An app in the list does not exist.")
			return false
		}
	}
	if err := tx.Commit(ctx); err != nil {
		errInternal(w, "assign roles", err)
		return false
	}
	return true
}

func (s *server) getUserRoles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == accounts.OwnerID {
		writeJSON(w, http.StatusOK, map[string]any{"roles": []assignment{{Role: "superadmin"}}, "owner": true})
		return
	}
	list, err := s.assignmentsOf(r.Context(), "account_id", id)
	if err != nil {
		errInternal(w, "load assignments", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": list})
}

func (s *server) putUserRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if id == accounts.OwnerID {
		writeError(w, http.StatusForbidden, "owner", "The owner is always a super admin.")
		return
	}
	if _, err := accounts.Load(ctx, s.d.Pool, id); errors.Is(err, accounts.ErrNotFound) {
		errNotFound(w, r)
		return
	} else if err != nil {
		errInternal(w, "load user", err)
		return
	}
	var in struct {
		Roles []assignment `json:"roles"`
	}
	if !readJSON(w, r, &in) || !s.replaceAssignments(w, r, "account_id", id, in.Roles) {
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "user.roles_changed", clientIP(ctx), map[string]any{"id": id, "roles": in.Roles})
	writeJSON(w, http.StatusOK, map[string]any{"roles": in.Roles})
}

// --- groups ---

type groupView struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Members     []string     `json:"members"`
	Roles       []assignment `json:"roles"`
}

func (s *server) listGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.d.Pool.Query(ctx, `select g.id::text, g.name, g.description,
		coalesce(array_agg(m.account_id order by m.account_id) filter (where m.account_id is not null), '{}')
		from groups g left join group_members m on m.group_id = g.id group by g.id order by g.name`)
	if err != nil {
		errInternal(w, "list groups", err)
		return
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (groupView, error) {
		var v groupView
		return v, r.Scan(&v.ID, &v.Name, &v.Description, &v.Members)
	})
	if err != nil {
		errInternal(w, "list groups", err)
		return
	}
	for i := range list {
		if list[i].Roles, err = s.assignmentsOf(ctx, "group_id", list[i].ID); err != nil {
			errInternal(w, "group roles", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": list})
}

type groupInput struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Members     *[]string `json:"members"`
}

func readGroup(w http.ResponseWriter, r *http.Request) (groupInput, bool) {
	var in groupInput
	if !readJSON(w, r, &in) {
		return in, false
	}
	in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if !roleNameRe.MatchString(in.Name) {
		writeInvalid(w, "name", "Give the group a name of at most 60 characters.")
		return in, false
	}
	if utf8.RuneCountInString(in.Description) > 200 {
		writeInvalid(w, "description", "At most 200 characters.")
		return in, false
	}
	return in, true
}

// setMembers replaces a group's members. When the group carries roles, the
// actor must hold their permissions: membership hands them out.
func (s *server) setMembers(w http.ResponseWriter, r *http.Request, tx pgx.Tx, id string, members []string) bool {
	ctx := r.Context()
	list, err := accounts.List(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "list accounts", err)
		return false
	}
	known := map[string]bool{}
	for _, a := range list {
		known[a.ID] = true
	}
	for _, m := range members {
		if !known[m] {
			writeInvalid(w, "members", "Choose people who have an account.")
			return false
		}
	}
	// Membership also opens the apps and credential providers that name the
	// group: changing it needs the right to manage those.
	if g, err := s.grants(ctx); err != nil {
		errInternal(w, "load grants", err)
		return false
	} else {
		ref := "group:" + id
		var apps, providers int
		if err := s.d.Pool.QueryRow(ctx, `select (select count(*) from apps where $1 = any(access)),
			(select count(*) from vault_providers where $1 = any(access))`, ref).Scan(&apps, &providers); err != nil {
			errInternal(w, "group use", err)
			return false
		}
		if (apps > 0 && !g.Has(rbac.AppsWrite)) || (providers > 0 && !g.Has(rbac.VaultWrite)) {
			writeError(w, http.StatusForbidden, "escalation", "This group opens apps or credential providers you do not manage; you cannot change who is in it.")
			return false
		}
	}
	roles, err := s.assignmentsOf(ctx, "group_id", id)
	if err != nil {
		errInternal(w, "group roles", err)
		return false
	}
	if len(roles) > 0 {
		byID, _, err := s.loadRoles(ctx)
		g, gerr := s.grants(ctx)
		if err != nil || gerr != nil {
			errInternal(w, "check grants", errors.Join(err, gerr))
			return false
		}
		for _, a := range roles {
			if !g.Covers(byID[a.Role].Permissions) {
				writeError(w, http.StatusForbidden, "escalation", "This group gives "+byID[a.Role].Name+"; you cannot change who is in it.")
				return false
			}
		}
	}
	if _, err := tx.Exec(ctx, `delete from group_members where group_id = $1`, id); err != nil {
		errInternal(w, "group members", err)
		return false
	}
	for _, m := range members {
		if _, err := tx.Exec(ctx, `insert into group_members (group_id, account_id) values ($1, $2) on conflict do nothing`, id, m); err != nil {
			errInternal(w, "group members", err)
			return false
		}
	}
	return true
}

func (s *server) createGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, ok := readGroup(w, r)
	if !ok {
		return
	}
	tx, err := s.d.Pool.Begin(ctx)
	if err != nil {
		errInternal(w, "create group", err)
		return
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var id string
	err = tx.QueryRow(ctx, `insert into groups (name, description) values ($1, $2) returning id::text`, in.Name, in.Description).Scan(&id)
	if isUnique(err) {
		writeInvalid(w, "name", "A group with this name exists.")
		return
	}
	if err != nil {
		errInternal(w, "create group", err)
		return
	}
	if in.Members != nil && !s.setMembers(w, r, tx, id, *in.Members) {
		return
	}
	if err := tx.Commit(ctx); err != nil {
		errInternal(w, "create group", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "group.created", clientIP(ctx), map[string]any{"id": id, "name": in.Name})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *server) updateGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	in, ok := readGroup(w, r)
	if !ok {
		return
	}
	tx, err := s.d.Pool.Begin(ctx)
	if err != nil {
		errInternal(w, "update group", err)
		return
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	tag, err := tx.Exec(ctx, `update groups set name = $2, description = $3 where id = $1`, id, in.Name, in.Description)
	if isUnique(err) {
		writeInvalid(w, "name", "A group with this name exists.")
		return
	}
	if err != nil {
		errInternal(w, "update group", err)
		return
	}
	if tag.RowsAffected() == 0 {
		errNotFound(w, r)
		return
	}
	if in.Members != nil && !s.setMembers(w, r, tx, id, *in.Members) {
		return
	}
	if err := tx.Commit(ctx); err != nil {
		errInternal(w, "update group", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "group.updated", clientIP(ctx), map[string]any{"id": id, "name": in.Name, "members": in.Members})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	// Deleting a group takes its roles away from its members.
	if roles, err := s.assignmentsOf(ctx, "group_id", id); err != nil {
		errInternal(w, "group roles", err)
		return
	} else if len(roles) > 0 {
		byID, _, err := s.loadRoles(ctx)
		g, gerr := s.grants(ctx)
		if err != nil || gerr != nil {
			errInternal(w, "check grants", errors.Join(err, gerr))
			return
		}
		for _, a := range roles {
			if !g.Covers(byID[a.Role].Permissions) {
				writeError(w, http.StatusForbidden, "escalation", "This group gives "+byID[a.Role].Name+"; you cannot delete it.")
				return
			}
		}
	}
	var name string
	err := s.d.Pool.QueryRow(ctx, `delete from groups where id = $1 returning name`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "delete group", err)
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `update apps set access = array_remove(access, $1) where $1 = any(access)`, "group:"+id); err != nil {
		errInternal(w, "drop group from apps", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "group.deleted", clientIP(ctx), map[string]any{"id": id, "name": name})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) putGroupRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	var exists bool
	if err := s.d.Pool.QueryRow(ctx, `select exists(select 1 from groups where id = $1)`, id).Scan(&exists); err != nil {
		errInternal(w, "load group", err)
		return
	}
	if !exists {
		errNotFound(w, r)
		return
	}
	var in struct {
		Roles []assignment `json:"roles"`
	}
	if !readJSON(w, r, &in) || !s.replaceAssignments(w, r, "group_id", id, in.Roles) {
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "group.roles_changed", clientIP(ctx), map[string]any{"id": id, "roles": in.Roles})
	writeJSON(w, http.StatusOK, map[string]any{"roles": in.Roles})
}

// appAdmin lets a request on /apps/{id} through when the account may change
// that app: apps.write globally, or as its application admin.
func (s *server) appAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := appID(w, r)
		if !ok {
			return
		}
		g, err := s.grants(r.Context())
		if err != nil {
			errInternal(w, "load grants", err)
			return
		}
		if !g.HasFor(rbac.AppsWrite, id) {
			writeError(w, http.StatusForbidden, "forbidden", "You do not have permission to change this app.")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), keyGrants, g)))
	}
}

// overview is the admin dashboard: counts, sign-ins per day, recent events
// and the most used apps over the last week.
func (s *server) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, err := s.grants(ctx)
	if err != nil {
		errInternal(w, "load grants", err)
		return
	}
	out := map[string]any{}
	var users, groups, appsN int
	if err := s.d.Pool.QueryRow(ctx, `select (select count(*) from users) + (select count(*) from owner),
		(select count(*) from groups), (select count(*) from apps)`).Scan(&users, &groups, &appsN); err != nil {
		errInternal(w, "overview", err)
		return
	}
	out["users"], out["groups"], out["apps"] = users, groups, appsN
	if sessions, err := s.d.Sessions.List(ctx); err == nil {
		out["sessions"] = len(sessions)
	}
	if g.Has(rbac.AuditRead) {
		since := time.Now().AddDate(0, 0, -6).Truncate(24 * time.Hour)
		type day struct {
			Day       string `json:"day"`
			Successes int    `json:"successes"`
			Failures  int    `json:"failures"`
			AppOpens  int    `json:"app_opens"`
		}
		rows, err := s.d.Pool.Query(ctx, `select to_char(d, 'YYYY-MM-DD'),
			count(a.*) filter (where a.action = 'login.success'),
			count(a.*) filter (where a.action = 'login.failure'),
			count(a.*) filter (where a.action = 'app.access')
			from generate_series($1::date, current_date, '1 day') d
			left join audit_log a on a.at >= d and a.at < d + interval '1 day'
			group by d order by d`, since)
		if err == nil {
			days, cerr := pgx.CollectRows(rows, func(r pgx.CollectableRow) (day, error) {
				var x day
				return x, r.Scan(&x.Day, &x.Successes, &x.Failures, &x.AppOpens)
			})
			if cerr == nil {
				out["days"] = days
			}
		}
		type top struct {
			App   string `json:"app"`
			Opens int    `json:"opens"`
		}
		rows, err = s.d.Pool.Query(ctx, `select detail->>'app', count(*) from audit_log
			where action = 'app.access' and at > now() - interval '7 days' and detail ? 'app'
			group by 1 order by 2 desc limit 8`)
		if err == nil {
			tops, cerr := pgx.CollectRows(rows, func(r pgx.CollectableRow) (top, error) {
				var x top
				return x, r.Scan(&x.App, &x.Opens)
			})
			if cerr == nil {
				out["top_apps"] = tops
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
