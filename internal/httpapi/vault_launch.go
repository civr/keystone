package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/crypto"
)

// Signing in to a vault app for you. A browser lets no page fill in another
// site's fields, but it may post a form there: Keystone sends the app's own
// login form with the stored username and password, as the person would.
// This works for classic form logins (routers, NAS pages) and for login APIs
// that accept a JSON body without a JSON content type.
//
// The SPA asks for a one-time launch (POST, within the vault's reconfirm
// window), then navigates to /vault/launch/<token>, whose page posts the
// form. The token lives a minute, is used once and belongs to the session.

const launchTTL = time.Minute

type launch struct {
	App     string `json:"app"`
	Session string `json:"session"`
}

func launchKey(tok string) string { return "vaultlaunch:" + tok }

// vaultLaunch hands out a one-time launch for a vault app that signs in by form.
func (s *server) vaultLaunch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, ok := s.vaultApp(w, r)
	if !ok {
		return
	}
	if v := a.Config.Vault; v == nil || (v.Mode != apps.VaultForm && v.Mode != apps.VaultJSON) {
		writeInvalid(w, "config.vault.mode", "This app copies the password instead.")
		return
	}
	if !vaultOpen(*SessionFrom(ctx), time.Duration(settingsFrom(ctx).Vault.ReconfirmMinutes)*time.Minute, time.Now()) {
		writeError(w, http.StatusForbidden, "stepup_required", "Confirm this with your passkey first.")
		return
	}
	tok, err := randomToken()
	if err != nil {
		errInternal(w, "launch token", err)
		return
	}
	raw, _ := json.Marshal(launch{App: a.ID, Session: SessionFrom(ctx).ID})
	if err := s.d.Valkey.Set(ctx, launchKey(tok), raw, launchTTL).Err(); err != nil {
		errUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "/vault/launch/" + tok})
}

// vaultLaunchPage posts the app's login form once.
func (s *server) vaultLaunchPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	gone := func() {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "This sign-in link was used or has expired. Open the app from Keystone again.", http.StatusGone)
	}
	raw, err := s.d.Valkey.GetDel(ctx, launchKey(chi.URLParam(r, "tok"))).Bytes()
	if errors.Is(err, redis.Nil) {
		gone()
		return
	}
	if err != nil {
		errUnavailable(w)
		return
	}
	var l launch
	if json.Unmarshal(raw, &l) != nil || l.Session != SessionFrom(ctx).ID {
		gone()
		return
	}
	a, err := apps.ByID(ctx, s.d.Pool, l.App)
	open := vaultOpen(*SessionFrom(ctx), time.Duration(settingsFrom(ctx).Vault.ReconfirmMinutes)*time.Minute, time.Now())
	if err != nil || a.Protocol != apps.Vault || a.Config.Vault == nil || !s.mayUse(ctx, a) || lacksMFA(ctx, a) || !open {
		gone()
		return
	}
	var user string
	var sealed *string
	err = s.d.Pool.QueryRow(ctx, `select username, password_sealed from vault_entries where app_id = $1`, a.ID).Scan(&user, &sealed)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && sealed == nil) {
		http.Error(w, "No password is stored for this app.", http.StatusNotFound)
		return
	}
	if err != nil {
		errInternal(w, "vault launch", err)
		return
	}
	pw, err := crypto.Open(s.d.Secrets.VaultKey, *sealed)
	if err != nil {
		errInternal(w, "vault open", err)
		return
	}
	v := a.Config.Vault
	page := launchPage{URL: v.LoginURL, Name: a.Name}
	if v.Mode == apps.VaultJSON {
		// text/plain sends name=value: the name holds the JSON up to a last
		// dummy field whose value closes it, e.g. {"u":"x","p":"y","_":"="}.
		body, _ := json.Marshal(map[string]string{v.UserField: user, v.PassField: string(pw)})
		page.Plain = true
		page.Fields = []launchField{{Name: strings.TrimSuffix(string(body), "}") + `,"_":"`, Value: `"}`}}
	} else {
		page.Fields = []launchField{{Name: v.UserField, Value: user}, {Name: v.PassField, Value: string(pw)}}
	}
	s.d.Audit.Log(ctx, actor(ctx), "vault.revealed", clientIP(ctx), map[string]any{"id": a.ID, "app": a.Name, "method": "launch"})
	u, _ := url.Parse(v.LoginURL)
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; script-src '"+launchScriptHash()+"'; style-src 'unsafe-inline'; "+
		"form-action "+u.Scheme+"://"+u.Host+"; frame-ancestors 'none'; base-uri 'none'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	_ = launchTmpl.Execute(w, page)
}

type launchField struct{ Name, Value string }

type launchPage struct {
	URL, Name string
	Plain     bool
	Fields    []launchField
}

const launchScript = `document.forms[0].submit()`

func launchScriptHash() string {
	sum := sha256.Sum256([]byte(launchScript))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

var launchTmpl = template.Must(template.New("launch").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="robots" content="noindex"><title>Signing in to {{.Name}}…</title></head>
<body style="font-family:system-ui,sans-serif;color:#6e6e73;display:grid;place-items:center;min-height:90vh">
<form method="post" action="{{.URL}}"{{if .Plain}} enctype="text/plain"{{end}}>
{{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{end}}<p>Signing in to {{.Name}}…</p>
<noscript><button type="submit">Continue</button></noscript>
</form>
<script>` + launchScript + `</script>
</body></html>`))
