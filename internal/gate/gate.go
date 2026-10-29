// Package gate is keystone-gate (spec §2, §10): the forward-auth endpoint a
// reverse proxy (Traefik, Caddy, nginx) asks before passing a request to an
// app configured with protocol gate.
//
//	GET /verify                 200 + X-Keystone-* headers, or 302 to Keystone
//	GET /verify?mode=nginx      as above, but 401 instead of 302 (nginx auth_request)
//	GET /login-redirect?rd=URL  the 302 for nginx's error_page 401
//	GET /.keystone/signin?rd=/  sign the browser in to the app (apps.GateSignIn)
package gate

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/gatetoken"
	"github.com/civr/keystone/internal/realip"
	"github.com/civr/keystone/internal/runtime"
	"github.com/civr/keystone/internal/session"
)

// Source supplies the current settings, apps and owner.
type Source interface {
	Get(ctx context.Context) (runtime.View, error)
}

// Server answers forward-auth checks.
type Server struct {
	View     Source
	Valkey   *redis.Client
	Sessions *session.Store
	Limiter  *session.Limiter
	// Pool, VaultKey and Audit serve sign-in: the stored credentials, and a
	// record of their use. Audit may be nil.
	Pool     *pgxpool.Pool
	VaultKey []byte
	Audit    *audit.Writer

	cache credsCache
}

// Handler returns the gate's HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// nginx auth_request keeps the client's method, so a POST to the app must not 405 here.
	mux.HandleFunc("/verify", s.verify)
	mux.HandleFunc("GET "+signinPath+"signin", s.signIn)
	mux.HandleFunc("GET /login-redirect", s.loginRedirect)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

// original reconstructs the URL the browser asked the proxy for. Traefik and
// Caddy set X-Forwarded-Proto/Host/Uri themselves but pass other client
// headers through, so X-Original-URL (set by nginx's auth_request config) is
// only read in nginx mode.
func original(r *http.Request) (*url.URL, bool) {
	if r.URL.Query().Get("mode") == "nginx" {
		raw := r.Header.Get("X-Original-URL")
		u, err := url.Parse(raw)
		return u, err == nil && u.Host != "" && u.User == nil
	}
	proto, host := r.Header.Get("X-Forwarded-Proto"), r.Header.Get("X-Forwarded-Host")
	if proto != "http" && proto != "https" || host == "" {
		return nil, false
	}
	u, err := url.Parse(proto + "://" + host + r.Header.Get("X-Forwarded-Uri"))
	if err != nil || u.User != nil {
		return nil, false
	}
	return u, true
}

// bypassed reports whether p falls under one of the app's exception prefixes.
// The path is decoded and cleaned first so "/api/../x" is not "/api/".
func bypassed(p string, prefixes []string) bool {
	clean := path.Clean("/" + p)
	for _, pre := range prefixes {
		base := strings.TrimSuffix(pre, "/")
		if clean == base || strings.HasPrefix(clean, base+"/") {
			return true
		}
	}
	return false
}

// trustedProxies parses network.trusted_proxies; invalid entries are skipped.
func trustedProxies(cidrs []string) []netip.Prefix {
	var out []netip.Prefix
	for _, c := range cidrs {
		if p, err := netip.ParsePrefix(c); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// fromProxy reports whether the direct peer is a trusted reverse proxy; only
// then are the forwarded headers, and so the app host, believable.
func fromProxy(r *http.Request, trusted []netip.Prefix) bool {
	ip := realip.ClientIP(r.RemoteAddr, "", nil)
	return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(ip) })
}

// clientIP is the browser's address as the trusted proxies report it.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	return realip.ClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), trusted).String()
}

func startURL(publicURL, rd string) string {
	return strings.TrimRight(publicURL, "/") + "/api/gate/start?rd=" + url.QueryEscape(rd)
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v, err := s.View.Get(ctx)
	if err != nil {
		slog.Error("gate: load view", "err", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	u, ok := original(r)
	if !ok {
		http.Error(w, "missing forwarded headers", http.StatusBadRequest)
		return
	}
	trusted := trustedProxies(v.Settings.Network.TrustedProxies)
	host := strings.ToLower(u.Hostname())
	var app *apps.App
	for i, a := range v.Apps {
		if a.Protocol == apps.GateP && a.Config.Gate != nil && a.Config.Gate.Host == host {
			app = &v.Apps[i]
		}
	}
	if app == nil {
		http.Error(w, "unknown app", http.StatusForbidden)
		return
	}
	if bypassed(u.Path, app.Config.Gate.BypassPaths) {
		w.WriteHeader(http.StatusOK)
		return
	}
	if c, err := r.Cookie(gatetoken.Cookie); err == nil {
		sess, err := gatetoken.Resolve(ctx, s.Valkey, s.Sessions, v.Settings, c.Value, time.Now())
		if err == nil {
			who, ok := v.Account(sess.UserID)
			if !ok || !app.AllowedFor(who) || (app.Config.RequireMFA && !sess.MFA()) {
				http.Error(w, "no access to this app", http.StatusForbidden)
				return
			}
			acct := app.Mapped(who)
			h := w.Header()
			h.Set("X-Keystone-User", acct.Username)
			h.Set("X-Keystone-Email", acct.Email)
			h.Set("X-Keystone-Name", acct.DisplayName)
			window := time.Duration(v.Settings.Vault.ReconfirmMinutes) * time.Minute
			proxy := fromProxy(r, trusted)
			if !proxy && app.Config.Gate.SignIn != nil {
				slog.Warn("gate: peer is not a trusted proxy, no credentials sent", "peer", r.RemoteAddr)
			}
			if err := s.addHeaders(ctx, h, app, sess, window, proxy); err != nil {
				slog.Error("gate: credentials", "err", err)
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			if redirect, err := s.wantsSignIn(r, u, app, sess, window, proxy); err != nil {
				slog.Error("gate: credentials", "err", err)
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			} else if redirect {
				h.Set("Cache-Control", "no-store")
				http.Redirect(w, r, signInURL(u), http.StatusFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		if err != gatetoken.ErrNotFound {
			slog.Error("gate: resolve", "err", err)
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	if ok, err := s.Limiter.Allow(ctx, "gate-ip", clientIP(r, trusted), v.Settings.Gate.FailPerIPPerMinute); err == nil && !ok {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("mode") == "nginx" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, startURL(v.Settings.PublicURL, u.String()), http.StatusFound)
}

// loginRedirect serves nginx's error_page: Keystone validates rd itself.
func (s *Server) loginRedirect(w http.ResponseWriter, r *http.Request) {
	v, err := s.View.Get(r.Context())
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, startURL(v.Settings.PublicURL, r.URL.Query().Get("rd")), http.StatusFound)
}
