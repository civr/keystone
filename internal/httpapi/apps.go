package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/rbac"
	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/appicon"
	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/crypto"
)

const (
	maxAppNameRunes = 40
	maxAppURLLen    = 2048
	// maxAppBody leaves room for a 64 kB icon plus JSON escaping.
	maxAppBody = 2 * maxIconLen
	maxIconLen = 64 << 10 // whole data URL
)

var (
	colorRe    = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	iconRe     = regexp.MustCompile(`^data:image/(png|svg\+xml|webp);base64,([A-Za-z0-9+/]+={0,2})$`)
	svgEventRe = regexp.MustCompile(`(?i)on[a-z]+\s*=`)
)

// appInput is the editable part of an app tile.
type appInput struct {
	Name     string      `json:"name"`
	URL      string      `json:"url"`
	Color    string      `json:"color"`
	Icon     string      `json:"icon"`
	Protocol string      `json:"protocol"`
	Config   apps.Config `json:"config"`
	// Access lists the account ids that may use the app; null is everyone.
	Access []string `json:"access"`
}

// validate trims and checks the input; it returns the offending field and an
// English message, or "" when valid.
func (a *appInput) validate() (field, msg string) {
	a.Name = strings.TrimSpace(a.Name)
	if n := utf8.RuneCountInString(a.Name); n < 1 || n > maxAppNameRunes {
		return "name", "The name must be 1 to 40 characters."
	}
	u, err := url.Parse(a.URL)
	if err != nil || len(a.URL) > maxAppURLLen || u.Host == "" ||
		(!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return "url", "The URL must be absolute and start with http:// or https://."
	}
	if a.Protocol == "" {
		a.Protocol = apps.Link
	}
	if f, m := a.Config.Validate(a.Protocol); f != "" {
		return f, m
	}
	if !colorRe.MatchString(a.Color) {
		return "color", "The color must be in the form #RRGGBB."
	}
	if a.Icon != "" {
		if msg := iconError(a.Icon); msg != "" {
			return "icon", msg
		}
	}
	return "", ""
}

// iconError checks an icon data URL; it returns an English message or "".
func iconError(icon string) string {
	if len(icon) > maxIconLen {
		return "The icon can be at most 64 kB."
	}
	m := iconRe.FindStringSubmatch(icon)
	if m == nil {
		return "The icon must be a base64 data URL (png, svg or webp)."
	}
	raw, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return "The icon is not valid base64."
	}
	if m[1] == "svg+xml" && unsafeSVG(string(raw)) {
		return "The SVG icon contains scripts or unsafe attributes."
	}
	return ""
}

// fetchAppIcon fetches the icon an app offers on its own address, vetted
// like an uploaded icon.
func (s *server) fetchAppIcon(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		writeInvalid(w, "url", "The URL must be absolute and start with http:// or https://.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	icon, err := appicon.Fetch(ctx, u.String(), func(d string) bool { return iconError(d) == "" })
	if err != nil {
		writeError(w, http.StatusNotFound, "no_icon", "This app does not offer a usable icon. Choose one yourself.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"icon": icon})
}

// unsafeSVG is a defence-in-depth blacklist. The real protection is that icons
// are only ever rendered through <img src="data:...">, where browsers never run
// scripts in an SVG (Task 10 must honor this: never inline an icon in the DOM).
func unsafeSVG(svg string) bool {
	if !utf8.ValidString(svg) || strings.HasPrefix(svg, "\ufeff") || strings.ContainsRune(svg, 0) {
		return true
	}
	l := strings.ToLower(svg)
	for _, bad := range []string{
		"&#", "<!entity", "<!doctype", "<script", ":script", "foreignobject",
		`attributename="on`, `attributename='on`, "javascript:", "data:", "<iframe", "<embed", "<object",
	} {
		if strings.Contains(l, bad) {
			return true
		}
	}
	return svgEventRe.MatchString(svg)
}

type appOut struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	URL       string      `json:"url"`
	Color     string      `json:"color"`
	Icon      string      `json:"icon"`
	Protocol  string      `json:"protocol"`
	Config    apps.Config `json:"config"`
	Access    []string    `json:"access"`
	HasSecret bool        `json:"has_secret"`
	Sort      int         `json:"sort"`
	CreatedAt time.Time   `json:"created_at"`
}

const appCols = `id::text, name, url, color, coalesce(icon, ''), protocol, config, access, sort, created_at`

// scanApp reads a row and strips secret hashes from the config.
func scanApp(r pgx.CollectableRow) (appOut, error) {
	var a appOut
	if err := r.Scan(&a.ID, &a.Name, &a.URL, &a.Color, &a.Icon, &a.Protocol, &a.Config, &a.Access, &a.Sort, &a.CreatedAt); err != nil {
		return a, err
	}
	if o := a.Config.OIDC; o != nil {
		a.HasSecret, o.SecretHash = o.SecretHash != "", ""
	}
	if l := a.Config.LDAP; l != nil {
		a.HasSecret, l.BindSecretHash = l.BindSecretHash != "", ""
	}
	return a, nil
}

// prepareConfig carries server-owned values (client ID, secret hashes) over
// from the stored app (prev, zero for a new app), generates a client ID for
// a new OIDC app, and checks that identifiers are unique across apps.
func (s *server) prepareConfig(ctx context.Context, w http.ResponseWriter, id string, in *appInput) bool {
	all, err := apps.Load(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "load apps", err)
		return false
	}
	var prev apps.Config
	for _, a := range all {
		if a.ID == id {
			prev = a.Config
		}
	}
	c := &in.Config
	if c.OIDC != nil {
		c.OIDC.ClientID, c.OIDC.SecretHash = uuid.NewString(), ""
		if prev.OIDC != nil {
			c.OIDC.ClientID, c.OIDC.SecretHash = prev.OIDC.ClientID, prev.OIDC.SecretHash
		}
	}
	if c.LDAP != nil {
		c.LDAP.BindSecretHash = ""
		if prev.LDAP != nil {
			c.LDAP.BindSecretHash = prev.LDAP.BindSecretHash
		}
	}
	for _, a := range all {
		if a.ID == id {
			continue
		}
		o := a.Config
		switch {
		case c.SAML != nil && o.SAML != nil && o.SAML.EntityID == c.SAML.EntityID:
			writeInvalid(w, "config.saml.entity_id", "Another app already uses this entity ID.")
		case c.LDAP != nil && o.LDAP != nil && o.LDAP.Slug == c.LDAP.Slug:
			writeInvalid(w, "config.ldap.slug", "Another app already uses this name.")
		case c.Gate != nil && o.Gate != nil && o.Gate.Host == c.Gate.Host:
			writeInvalid(w, "config.gate.host", "Another app already uses this hostname.")
		default:
			continue
		}
		return false
	}
	return true
}

// appSecret generates a new OIDC client secret or LDAP bind password for an
// app and returns it once; only its hash is stored.
func (s *server) appSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	a, err := apps.ByID(ctx, s.d.Pool, id)
	if errors.Is(err, apps.ErrNotFound) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "load app", err)
		return
	}
	if a.Config.OIDC == nil && a.Config.LDAP == nil {
		writeInvalid(w, "protocol", "Only OIDC and LDAP apps have a secret.")
		return
	}
	secret, err := randomToken()
	if err != nil {
		errInternal(w, "app secret", err)
		return
	}
	hash, err := crypto.HashSecret(secret)
	if err != nil {
		errInternal(w, "app secret", err)
		return
	}
	if a.Config.OIDC != nil {
		a.Config.OIDC.SecretHash = hash
	} else {
		a.Config.LDAP.BindSecretHash = hash
	}
	if _, err := s.d.Pool.Exec(ctx, `update apps set config = $2 where id = $1`, id, a.Config); err != nil {
		errInternal(w, "app secret", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "app.secret_created", clientIP(ctx), map[string]any{"id": id, "name": a.Name})
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret})
}

// listApps lists the apps. Members, and anyone asking with ?mine=1 (the home
// page), get only the apps they may use; admins managing apps get all.
func (s *server) listApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	acct, err := s.account(ctx)
	if err != nil {
		errInternal(w, "load account", err)
		return
	}
	g, err := s.grants(ctx)
	if err != nil {
		errInternal(w, "load grants", err)
		return
	}
	all := g.Has(rbac.AppsRead) && r.URL.Query().Get("mine") == ""
	groups := make([]string, 0, len(acct.Groups))
	for _, g := range acct.Groups {
		groups = append(groups, "group:"+g)
	}
	rows, err := s.d.Pool.Query(ctx, `select `+appCols+` from apps where $1 or access is null or $2 = any(access) or access && $3::text[]
		order by sort, created_at, id`, all, acct.ID, groups)
	if err != nil {
		errInternal(w, "list apps", err)
		return
	}
	list, err := pgx.CollectRows(rows, scanApp)
	if err != nil {
		errInternal(w, "list apps", err)
		return
	}
	if !all {
		// Members get the tile, not how the app is set up or who else uses
		// it; only whether a SAML tile signs in directly (tileHref).
		for i := range list {
			cfg := apps.Config{}
			if sm := list[i].Config.SAML; sm != nil {
				cfg.SAML = &apps.SAML{IdPInitiated: sm.IdPInitiated}
			}
			if v := list[i].Config.Vault; v != nil {
				cfg.Vault = &apps.VaultLogin{Mode: v.Mode} // how the tile opens
			}
			list[i].Config, list[i].Access, list[i].HasSecret = cfg, nil, false
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": list})
}

func (s *server) readApp(w http.ResponseWriter, r *http.Request) (appInput, bool) {
	var in appInput
	if !readJSONLimit(w, r, &in, maxAppBody) {
		return in, false
	}
	if f, m := in.validate(); f != "" {
		writeInvalid(w, f, m)
		return in, false
	}
	return in, true
}

func (s *server) createApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, ok := s.readApp(w, r)
	if !ok || !s.prepareConfig(ctx, w, "", &in) || !s.checkAccess(ctx, w, in.Access) || !s.overrideFree(ctx, w, in.Config.Account) {
		return
	}
	// A shared vault password is the owner's until an admin says otherwise.
	if in.Protocol == apps.Vault && in.Access == nil {
		in.Access = []string{accounts.OwnerID}
	}
	rows, err := s.d.Pool.Query(ctx, `insert into apps (id, name, url, color, icon, protocol, config, access, sort, created_at)
		values ($1, $2, $3, $4, nullif($5, ''), $6, $7, $8, (select coalesce(max(sort) + 1, 0) from apps), now())
		returning `+appCols, uuid.NewString(), in.Name, in.URL, in.Color, in.Icon, in.Protocol, in.Config, in.Access)
	if err != nil {
		errInternal(w, "create app", err)
		return
	}
	a, err := pgx.CollectOneRow(rows, scanApp)
	if err != nil {
		errInternal(w, "create app", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "app.created", clientIP(ctx), map[string]any{"id": a.ID, "name": a.Name})
	writeJSON(w, http.StatusCreated, a)
}

// overrideFree checks that the owner's name at the app (an account
// override) is not another account's username: that person would be the
// owner at the app. False when an answer was written.
func (s *server) overrideFree(ctx context.Context, w http.ResponseWriter, acct apps.Account) bool {
	name, email := acct.Username, acct.Email
	if name == "" && email == "" {
		return true
	}
	list, err := accounts.List(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "list accounts", err)
		return false
	}
	for _, a := range list {
		if a.IsOwner() {
			continue
		}
		if name != "" && strings.EqualFold(a.Username, name) {
			writeInvalid(w, "config.account.username", "Another user has this username. Choose another name for the owner at this app.")
			return false
		}
		if email != "" && strings.EqualFold(a.Email, email) {
			writeInvalid(w, "config.account.email", "Another user has this e-mail address. Choose another for the owner at this app.")
			return false
		}
	}
	return true
}

// checkAccess checks that every id in access is an account; false when an
// answer was written. nil (everyone) is always fine.
func (s *server) checkAccess(ctx context.Context, w http.ResponseWriter, access []string) bool {
	if access == nil {
		return true
	}
	list, err := accounts.List(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "list accounts", err)
		return false
	}
	known := map[string]bool{}
	for _, a := range list {
		known[a.ID] = true
	}
	rows, err := s.d.Pool.Query(ctx, `select 'group:' || id::text from groups`)
	if err != nil {
		errInternal(w, "list groups", err)
		return false
	}
	groups, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		errInternal(w, "list groups", err)
		return false
	}
	for _, g := range groups {
		known[g] = true
	}
	for _, id := range access {
		if !known[id] {
			writeInvalid(w, "access", "Choose people or groups that exist.")
			return false
		}
	}
	return true
}

func appID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errNotFound(w, r)
		return "", false
	}
	return id.String(), true
}

func (s *server) updateApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	in, ok := s.readApp(w, r)
	if !ok || !s.prepareConfig(ctx, w, id, &in) || !s.checkAccess(ctx, w, in.Access) || !s.overrideFree(ctx, w, in.Config.Account) {
		return
	}
	rows, err := s.d.Pool.Query(ctx, `update apps set name = $2, url = $3, color = $4, icon = nullif($5, ''), protocol = $6, config = $7,
		access = $8 where id = $1 returning `+appCols, id, in.Name, in.URL, in.Color, in.Icon, in.Protocol, in.Config, in.Access)
	if err != nil {
		errInternal(w, "update app", err)
		return
	}
	a, err := pgx.CollectOneRow(rows, scanApp)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "update app", err)
		return
	}
	if a.Protocol != apps.Vault {
		// A stored password must not outlive the app's use of the vault.
		if _, err := s.d.Pool.Exec(ctx, `delete from vault_entries where app_id = $1`, a.ID); err != nil {
			errInternal(w, "clear vault", err)
			return
		}
	}
	s.d.Audit.Log(ctx, actor(ctx), "app.updated", clientIP(ctx), map[string]any{"id": a.ID, "name": a.Name})
	writeJSON(w, http.StatusOK, a)
}

func (s *server) deleteApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	var name string
	err := s.d.Pool.QueryRow(ctx, `delete from apps where id = $1 returning name`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "delete app", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "app.deleted", clientIP(ctx), map[string]any{"id": id, "name": name})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) reorderApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	bad := func() { writeInvalid(w, "ids", "ids must contain every app exactly once.") }
	ids := make([]string, len(req.IDs))
	seen := make(map[string]bool, len(ids))
	for i, raw := range req.IDs {
		u, err := uuid.Parse(raw)
		if err != nil || seen[u.String()] {
			bad()
			return
		}
		ids[i] = u.String()
		seen[ids[i]] = true
	}
	tx, err := s.d.Pool.Begin(ctx)
	if err != nil {
		errInternal(w, "reorder apps", err)
		return
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := tx.Query(ctx, `select id::text from apps for update`)
	if err != nil {
		errInternal(w, "reorder apps", err)
		return
	}
	existing, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		errInternal(w, "reorder apps", err)
		return
	}
	if len(existing) != len(ids) {
		bad()
		return
	}
	for _, id := range existing {
		if !seen[id] {
			bad()
			return
		}
	}
	if _, err := tx.Exec(ctx, `update apps a set sort = o.n - 1
		from unnest($1::uuid[]) with ordinality as o(id, n) where a.id = o.id`, ids); err != nil {
		errInternal(w, "reorder apps", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		errInternal(w, "reorder apps", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "app.reordered", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
