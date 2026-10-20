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

	"github.com/civr/keystone/internal/audit"
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
	svgEventRe = regexp.MustCompile(`(?i)[\s"'/]on[a-z]+\s*=`)
)

// appInput is the editable part of an app tile.
type appInput struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
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
	if !colorRe.MatchString(a.Color) {
		return "color", "De kleur moet de vorm #RRGGBB hebben."
	}
	if a.Icon != "" {
		if len(a.Icon) > maxIconLen {
			return "icon", "Het icoon mag maximaal 64 kB zijn."
		}
		m := iconRe.FindStringSubmatch(a.Icon)
		if m == nil {
			return "icon", "Het icoon moet een base64 data-URL zijn (png, svg of webp)."
		}
		raw, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil {
			return "icon", "Het icoon is geen geldige base64."
		}
		if m[1] == "svg+xml" && unsafeSVG(string(raw)) {
			return "icon", "Het SVG-icoon bevat scripts of gevaarlijke attributen."
		}
	}
	return "", ""
}

// unsafeSVG rejects <script, on*= event attributes and javascript: URLs.
func unsafeSVG(svg string) bool {
	l := strings.ToLower(svg)
	return strings.Contains(l, "<script") || strings.Contains(l, "javascript:") || svgEventRe.MatchString(svg)
}

type appOut struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Color     string    `json:"color"`
	Icon      string    `json:"icon"`
	Protocol  string    `json:"protocol"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"created_at"`
}

const appCols = `id::text, name, url, color, coalesce(icon, ''), protocol, sort, created_at`

func scanApp(r pgx.CollectableRow) (appOut, error) {
	var a appOut
	return a, r.Scan(&a.ID, &a.Name, &a.URL, &a.Color, &a.Icon, &a.Protocol, &a.Sort, &a.CreatedAt)
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
	if !ok {
		return
	}
	rows, err := s.d.Pool.Query(ctx, `insert into apps (id, name, url, color, icon, sort, created_at)
		values ($1, $2, $3, $4, nullif($5, ''), (select coalesce(max(sort) + 1, 0) from apps), now())
		returning `+appCols, uuid.NewString(), in.Name, in.URL, in.Color, in.Icon)
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
	if !ok {
		return
	}
	rows, err := s.d.Pool.Query(ctx, `update apps set name = $2, url = $3, color = $4, icon = nullif($5, '')
		where id = $1 returning `+appCols, id, in.Name, in.URL, in.Color, in.Icon)
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
