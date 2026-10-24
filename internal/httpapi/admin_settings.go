package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/settings"
)

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, settingsFrom(r.Context()))
}

func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := clientIP(ctx)
	var next settings.Settings
	if !readJSON(w, r, &next) {
		return
	}
	if err := next.Validate(ip); err != nil {
		writeSettingsError(w, err)
		return
	}
	// Admin routes only run after setup, where public_url is the anchor for
	// CSRF, cookies and passkeys: it can be changed but never cleared.
	if strings.TrimSpace(next.PublicURL) == "" {
		writeInvalid(w, "public_url", "The public address is required.")
		return
	}
	if next.Login.PasskeyRequired {
		var passkeys int
		if err := s.d.Pool.QueryRow(ctx, `select count(*) from passkeys`).Scan(&passkeys); err != nil {
			errInternal(w, "count passkeys", err)
			return
		}
		if passkeys == 0 {
			writeInvalid(w, "login.passkey_required", "Add a passkey first (this only works over https); otherwise you could not sign in anymore.")
			return
		}
	}
	cur := settingsFrom(ctx)
	if originOf(cur.PublicURL) != originOf(next.PublicURL) && r.URL.Query().Get("confirm") != "1" {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "confirm_required", "warning": "passkeys_rp_change",
			"message": "A different public address can make existing passkeys unusable and locks you out of this page until you open the new address. Confirm to continue.",
		})
		return
	}
	old, err := s.d.Settings.Put(ctx, next, ip)
	if err != nil {
		writeSettingsError(w, err)
		return
	}
	if changes := diffJSON(old, next); len(changes) > 0 {
		s.d.Audit.Log(ctx, actor(ctx), "settings.changed", ip, map[string]any{"changes": changes})
	}
	writeJSON(w, http.StatusOK, next)
}

func writeSettingsError(w http.ResponseWriter, err error) {
	var ve *settings.ValidationError
	if errors.As(err, &ve) {
		writeInvalid(w, ve.Field, ve.Message)
		return
	}
	errInternal(w, "save settings", err)
}

// originOf reduces publicURL to scheme://host[:port], lowercased and without
// the scheme's default port.
func originOf(publicURL string) string {
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme + "://" + normHost(scheme, u.Host)
}

type change struct {
	Path string `json:"path"`
	Old  any    `json:"old"`
	New  any    `json:"new"`
}

// diffJSON lists the leaf paths (dot notation) whose JSON value differs
// between old and next, sorted by path. Arrays are compared as a whole.
func diffJSON(old, next any) []change {
	a, b := toTree(old), toTree(next)
	out := []change{}
	var walk func(path string, x, y any)
	walk = func(path string, x, y any) {
		mx, okx := x.(map[string]any)
		my, oky := y.(map[string]any)
		if okx && oky {
			keys := map[string]struct{}{}
			for k := range mx {
				keys[k] = struct{}{}
			}
			for k := range my {
				keys[k] = struct{}{}
			}
			for k := range keys {
				p := k
				if path != "" {
					p = path + "." + k
				}
				walk(p, mx[k], my[k])
			}
			return
		}
		if !reflect.DeepEqual(x, y) {
			out = append(out, change{Path: path, Old: x, New: y})
		}
	}
	walk("", a, b)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func toTree(v any) any {
	raw, _ := json.Marshal(v)
	var t any
	_ = json.Unmarshal(raw, &t)
	return t
}

// --- Active Directory ---

// storedAD returns the stored AD configuration; ok is false when none exists.
func (s *server) storedAD(ctx context.Context) (cfg ad.Config, ok bool, err error) {
	var raw []byte
	err = s.d.Pool.QueryRow(ctx, `select data from ad_config where id = 1`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	return cfg, true, json.Unmarshal(raw, &cfg)
}

func (s *server) getAD(w http.ResponseWriter, r *http.Request) {
	cfg, ok, err := s.storedAD(r.Context())
	if err != nil {
		errInternal(w, "load ad config", err)
		return
	}
	if !ok {
		errNotFound(w, r)
		return
	}
	set := cfg.BindPasswordSealed != ""
	m := redactedAD(cfg)
	m["bind_password_set"] = set
	writeJSON(w, http.StatusOK, m)
}

// readAdminAD decodes and validates an AD body. A new bind_password is
// sealed; otherwise the stored sealed password is kept.
func (s *server) readAdminAD(w http.ResponseWriter, r *http.Request) (ad.Config, bool) {
	var req adRequest
	if !readJSON(w, r, &req) {
		return ad.Config{}, false
	}
	cfg := req.Config
	cfg.BindPasswordSealed = ""
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_ad_config", err.Error())
		return ad.Config{}, false
	}
	if req.BindPassword != "" {
		sealed, err := crypto.Seal(s.d.Secrets.DataKey, []byte(req.BindPassword))
		if err != nil {
			errInternal(w, "seal bind password", err)
			return ad.Config{}, false
		}
		cfg.BindPasswordSealed = sealed
		return cfg, true
	}
	stored, ok, err := s.storedAD(r.Context())
	if err != nil {
		errInternal(w, "load ad config", err)
		return ad.Config{}, false
	}
	if !ok || stored.BindPasswordSealed == "" {
		writeError(w, http.StatusBadRequest, "invalid_ad_config", "bind_password is required")
		return ad.Config{}, false
	}
	// The stored password is never sent to a different server.
	if stored.URL != cfg.URL {
		writeError(w, http.StatusBadRequest, "bind_password_required", "Enter the bind password again when you change the Active Directory server.")
		return ad.Config{}, false
	}
	cfg.BindPasswordSealed = stored.BindPasswordSealed
	return cfg, true
}

// redactedAD is cfg as a JSON tree without the sealed password.
func redactedAD(cfg ad.Config) map[string]any {
	cfg.BindPasswordSealed = ""
	m, _ := toTree(cfg).(map[string]any)
	delete(m, "bind_password_sealed")
	return m
}

func (s *server) putAD(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prev, hadPrev, err := s.storedAD(ctx)
	if err != nil {
		errInternal(w, "load ad config", err)
		return
	}
	cfg, ok := s.readAdminAD(w, r)
	if !ok {
		return
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		errInternal(w, "encode ad config", err)
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `insert into ad_config (id, data) values (1, $1)
		on conflict (id) do update set data = excluded.data`, raw); err != nil {
		errInternal(w, "save ad config", err)
		return
	}
	oldTree := map[string]any{}
	if hadPrev {
		oldTree = redactedAD(prev)
	}
	s.d.Audit.Log(ctx, actor(ctx), "ad.changed", clientIP(ctx), map[string]any{
		"changes":          diffJSON(oldTree, redactedAD(cfg)),
		"password_changed": !hadPrev || prev.BindPasswordSealed != cfg.BindPasswordSealed,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) testAD(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.readAdminAD(w, r)
	if !ok {
		return
	}
	test := s.d.TestAD
	if test == nil {
		test = func(ctx context.Context, c ad.Config) (int, error) { return ad.Test(ctx, c, s.d.Secrets.DataKey) }
	}
	n, err := test(r.Context(), cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "users_found": 0, "error": "ad_test_failed", "message": adTestMessage(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "users_found": n})
}

func adTestMessage(err error) string {
	switch {
	case errors.Is(err, ad.ErrInvalidCredentials):
		return "The bind user or password is wrong."
	case errors.Is(err, ad.ErrUnavailable):
		return "Active Directory cannot be reached."
	case errors.Is(err, ad.ErrNotFound):
		return "No users found with this base DN and filter."
	}
	return "The connection to Active Directory failed: " + err.Error()
}
