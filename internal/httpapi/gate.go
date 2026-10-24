package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/gatetoken"
)

// gateStart gives the browser a ks_gate cookie on the parent domain and sends
// it back to rd, an address of an app behind keystone-gate. keystone-gate
// redirects here when the cookie is missing.
func (s *server) gateStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	rd := r.URL.Query().Get("rd")
	u, err := url.Parse(rd)
	bad := func() {
		writeError(w, http.StatusBadRequest, "bad_redirect", "This address does not belong to an app behind the gate.")
	}
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Host == "" || cfg.Domain == "" {
		bad()
		return
	}
	secure := strings.HasPrefix(strings.ToLower(cfg.PublicURL), "https://")
	if secure && u.Scheme == "http" {
		// The cookie below is Secure: a browser on http:// never sends it
		// back, and would bounce between the gate and Keystone forever.
		writeError(w, http.StatusBadRequest, "insecure_app", "This app runs on http, but Keystone runs on https. Put the app behind https too.")
		return
	}
	host := strings.ToLower(u.Hostname())
	domain := strings.ToLower(cfg.Domain)
	if host != domain && !strings.HasSuffix(host, "."+domain) {
		bad()
		return
	}
	list, err := apps.Load(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "gate apps", err)
		return
	}
	var app *apps.App
	for i, a := range list {
		if a.Protocol == apps.GateP && a.Config.Gate != nil && a.Config.Gate.Host == host {
			app = &list[i]
		}
	}
	if app == nil {
		bad()
		return
	}
	if !app.Allowed(accountKey(SessionFrom(ctx).UserID)) {
		writeError(w, http.StatusForbidden, "no_access", "You do not have access to this app.")
		return
	}
	ttl := time.Duration(cfg.Session.DurationHours) * time.Hour
	tok, err := gatetoken.Issue(ctx, s.d.Valkey, SessionFrom(ctx).ID, ttl)
	if err != nil {
		errInternal(w, "gate token", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: gatetoken.Cookie, Value: tok, Domain: domain, Path: "/", MaxAge: int(ttl / time.Second),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	s.appAccess(ctx, *app, "gate")
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusFound)
}
