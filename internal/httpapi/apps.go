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

	"github.com/civr/keystone/internal/appicon"
	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/audit"
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
}

// validate trims and checks the input; it returns the offending field and a
// Dutch message, or "" when valid.
func (a *appInput) validate() (field, msg string) {
	a.Name = strings.TrimSpace(a.Name)
	if n := utf8.RuneCountInString(a.Name); n < 1 || n > maxAppNameRunes {
		return "name", "De naam moet 1 tot 40 tekens lang zijn."
	}
	u, err := url.Parse(a.URL)
	if err != nil || len(a.URL) > maxAppURLLen || u.Host == "" ||
		(!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
		return "url", "De URL moet absoluut zijn en met http:// of https:// beginnen."
	}
	if a.Protocol == "" {
		a.Protocol = apps.Link
	}
	if f, m := a.Config.Validate(a.Protocol); f != "" {
		return f, m
	}
	if !colorRe.MatchString(a.Color) {
		return "color", "De kleur moet de vorm #RRGGBB hebben."
	}
	if a.Icon != "" {
		if msg := iconError(a.Icon); msg != "" {
			return "icon", msg
		}
	}
	return "", ""
}

// iconError checks an icon data URL; it returns a Dutch message or "".
func iconError(icon string) string {
	if len(icon) > maxIconLen {
		return "Het icoon mag maximaal 64 kB zijn."
	}
	m := iconRe.FindStringSubmatch(icon)
	if m == nil {
		return "Het icoon moet een base64 data-URL zijn (png, svg of webp)."
	}
	raw, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return "Het icoon is geen geldige base64."
	}
	if m[1] == "svg+xml" && unsafeSVG(string(raw)) {
		return "Het SVG-icoon bevat scripts of gevaarlijke attributen."
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
		writeInvalid(w, "url", "De URL moet absoluut zijn en met http:// of https:// beginnen.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	icon, err := appicon.Fetch(ctx, u.String(), func(d string) bool { return iconError(d) == "" })
	if err != nil {
		writeError(w, http.StatusNotFound, "no_icon", "Deze app biedt geen bruikbaar icoon aan. Kies er zelf een.")
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
	HasSecret bool        `json:"has_secret"`
	Sort      int         `json:"sort"`
	CreatedAt time.Time   `json:"created_at"`
}

const appCols = `id::text, name, url, color, coalesce(icon, ''), protocol, config, sort, created_at`

// scanApp reads a row and strips secret hashes from the config.
func scanApp(r pgx.CollectableRow) (appOut, error) {
	var a appOut
	if err := r.Scan(&a.ID, &a.Name, &a.URL, &a.Color, &a.Icon, &a.Protocol, &a.Config, &a.Sort, &a.CreatedAt); err != nil {
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
			writeInvalid(w, "config.saml.entity_id", "Een andere app gebruikt deze entity ID al.")
		case c.LDAP != nil && o.LDAP != nil && o.LDAP.Slug == c.LDAP.Slug:
			writeInvalid(w, "config.ldap.slug", "Een andere app gebruikt deze naam al.")
		case c.Gate != nil && o.Gate != nil && o.Gate.Host == c.Gate.Host:
			writeInvalid(w, "config.gate.host", "Een andere app gebruikt deze hostnaam al.")
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
		writeInvalid(w, "protocol", "Alleen OIDC- en LDAP-apps hebben een geheim.")
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
	s.d.Audit.Log(ctx, audit.ActorOwner, "app.secret_created", clientIP(ctx), map[string]any{"id": id, "name": a.Name})
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret})
}

func (s *server) listApps(w http.ResponseWriter, r *http.Request) {
	rows, err := s.d.Pool.Query(r.Context(), `select `+appCols+` from apps order by sort, created_at, id`)
	if err != nil {
		errInternal(w, "list apps", err)
		return
	}
	apps, err := pgx.CollectRows(rows, scanApp)
	if err != nil {
		errInternal(w, "list apps", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
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
	if !ok || !s.prepareConfig(ctx, w, "", &in) {
		return
	}
	rows, err := s.d.Pool.Query(ctx, `insert into apps (id, name, url, color, icon, protocol, config, sort, created_at)
		values ($1, $2, $3, $4, nullif($5, ''), $6, $7, (select coalesce(max(sort) + 1, 0) from apps), now())
		returning `+appCols, uuid.NewString(), in.Name, in.URL, in.Color, in.Icon, in.Protocol, in.Config)
	if err != nil {
		errInternal(w, "create app", err)
		return
	}
	a, err := pgx.CollectOneRow(rows, scanApp)
	if err != nil {
		errInternal(w, "create app", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "app.created", clientIP(ctx), map[string]any{"id": a.ID, "name": a.Name})
	writeJSON(w, http.StatusCreated, a)
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
	if !ok || !s.prepareConfig(ctx, w, id, &in) {
		return
	}
	rows, err := s.d.Pool.Query(ctx, `update apps set name = $2, url = $3, color = $4, icon = nullif($5, ''), protocol = $6, config = $7
		where id = $1 returning `+appCols, id, in.Name, in.URL, in.Color, in.Icon, in.Protocol, in.Config)
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
	s.d.Audit.Log(ctx, audit.ActorOwner, "app.updated", clientIP(ctx), map[string]any{"id": a.ID, "name": a.Name})
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
	s.d.Audit.Log(ctx, audit.ActorOwner, "app.deleted", clientIP(ctx), map[string]any{"id": id, "name": name})
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
	bad := func() { writeInvalid(w, "ids", "ids moet alle apps precies één keer bevatten.") }
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
	s.d.Audit.Log(ctx, audit.ActorOwner, "app.reordered", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
