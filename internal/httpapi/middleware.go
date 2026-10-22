package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/civr/keystone/internal/realip"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

// SessionCookie is the name of the session cookie.
const SessionCookie = "ks_session"

type ctxKey int

const (
	keySettings ctxKey = iota
	keyClientIP
	keySession
)

// settingsFrom returns the settings snapshot taken at the start of the request.
func settingsFrom(ctx context.Context) settings.Settings {
	s, _ := ctx.Value(keySettings).(settings.Settings)
	return s
}

// clientIP returns the client address resolved by the realip middleware.
func clientIP(ctx context.Context) netip.Addr {
	ip, _ := ctx.Value(keyClientIP).(netip.Addr)
	return ip
}

// SessionFrom returns the session loaded for this request, or nil.
func SessionFrom(ctx context.Context) *session.Session {
	s, _ := ctx.Value(keySession).(*session.Session)
	return s
}

// middlewares returns the global chain in order: realip → security headers →
// request log → CIDR allowlists → Origin check → session loader →
// must-register-passkey gate.
func (s *server) middlewares() []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		s.realIP, s.securityHeaders, requestLog, cidrCheck, s.originCheck, s.loadSession, mustRegisterGate,
	}
}

// realIP snapshots the current settings (so changes apply on the next
// request) and resolves the client IP from RemoteAddr + X-Forwarded-For.
func (s *server) realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := s.d.Settings.Get(r.Context())
		if err != nil {
			slog.Error("http: load settings", "err", err)
			errUnavailable(w)
			return
		}
		trusted := parsePrefixes(cfg.Network.TrustedProxies)
		xff := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
		ip := realip.ClientIP(r.RemoteAddr, xff, trusted)
		ctx := context.WithValue(r.Context(), keySettings, cfg)
		ctx = context.WithValue(ctx, keyClientIP, ip)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := settingsFrom(r.Context())
		h := w.Header()
		h.Set("Content-Security-Policy", withExtra(s.csp, cfg.Headers.CSPExtra))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if cfg.Headers.FrameDeny {
			h.Set("X-Frame-Options", "DENY")
		}
		if cfg.Headers.HSTS && strings.HasPrefix(strings.ToLower(cfg.PublicURL), "https://") {
			v := "max-age=" + strconv.Itoa(cfg.Headers.HSTSMaxAge)
			if cfg.Headers.HSTSIncludeSubdomains {
				v += "; includeSubDomains"
			}
			if cfg.Headers.HSTSPreload {
				v += "; preload"
			}
			h.Set("Strict-Transport-Security", v)
		}
		next.ServeHTTP(w, r)
	})
}

// requestLog logs one line per request. Query strings are omitted because
// they may carry codes or tokens.
func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", status,
			"ms", time.Since(start).Milliseconds(), "ip", clientIP(r.Context()).String())
	})
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// under reports whether path is prefix itself or below it.
func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// pathUnder checks both the decoded and the raw path so an encoded variant
// (e.g. /api/%61dmin) cannot slip past the check while still being routed.
func pathUnder(r *http.Request, prefix string) bool {
	return under(r.URL.Path, prefix) || (r.URL.RawPath != "" && under(r.URL.RawPath, prefix))
}

// cidrCheck enforces network.admin_cidrs on /api/admin and on writes to
// /api/apps, and network.login_cidrs on /api/auth. An empty list allows all.
func cidrCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := settingsFrom(r.Context())
		var list []string
		switch {
		case pathUnder(r, "/api/admin"), pathUnder(r, "/api/apps") && !isSafeMethod(r.Method):
			list = cfg.Network.AdminCIDRs
		case pathUnder(r, "/api/auth"):
			list = cfg.Network.LoginCIDRs
		}
		if len(list) > 0 && !containsIP(parsePrefixes(list), clientIP(r.Context())) {
			slog.Warn("http: blocked by network allowlist", "path", r.URL.Path, "ip", clientIP(r.Context()).String())
			writeError(w, http.StatusForbidden, "ip_not_allowed", "Toegang vanaf dit netwerkadres is niet toegestaan.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originCheck is the CSRF guard: state-changing requests must carry an Origin
// equal to the origin of public_url, or — while public_url is unset during
// setup — whose host equals the request Host. Until setup is complete,
// /api/setup also accepts the request Host as origin, so a wrong or
// unreachable public_url saved in the domain step cannot lock the operator
// out of the wizard (the setup unlock cookie is still required there).
func (s *server) originCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) || originAllowed(r, settingsFrom(r.Context()).PublicURL) {
			next.ServeHTTP(w, r)
			return
		}
		if pathUnder(r, "/api/setup") && originAllowed(r, "") {
			done, err := s.setupDone(r.Context())
			if err != nil {
				slog.Error("http: setup state", "err", err)
				errUnavailable(w)
				return
			}
			if !done {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusForbidden, "bad_origin", "Verzoek geweigerd: het komt niet van deze Keystone-pagina.")
	})
}

// originAllowed reports whether the request Origin equals the origin of
// publicURL, or — when publicURL is empty — whose host equals the request Host.
func originAllowed(r *http.Request, publicURL string) bool {
	o, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || o.Host == "" || (o.Scheme != "http" && o.Scheme != "https") {
		return false
	}
	scheme := strings.ToLower(o.Scheme)
	got := normHost(scheme, o.Host)
	if publicURL == "" {
		return got == normHost(scheme, r.Host)
	}
	p, err := url.Parse(publicURL)
	if err != nil || p.Host == "" {
		return false
	}
	return scheme == strings.ToLower(p.Scheme) && got == normHost(scheme, p.Host)
}

// normHost lowercases host and drops the default port of scheme.
func normHost(scheme, host string) string {
	host = strings.ToLower(host)
	if (scheme == "https" && strings.HasSuffix(host, ":443")) || (scheme == "http" && strings.HasSuffix(host, ":80")) {
		host = host[:strings.LastIndexByte(host, ':')]
	}
	return host
}

// loadSession attaches the session named by the ks_session cookie, checked
// against the current settings. A missing or expired session is not an error
// here; routes that need one use requireSession. For sliding sessions the
// cookie is re-issued with a fresh Max-Age on the first request of each clock
// hour, so the browser keeps it as long as the server-side session lives.
func (s *server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(SessionCookie)
		if err != nil || c.Value == "" || s.d.Sessions == nil {
			next.ServeHTTP(w, r)
			return
		}
		cfg, now := settingsFrom(r.Context()), time.Now()
		sess, prevSeen, err := s.d.Sessions.GetSliding(r.Context(), c.Value, cfg, now)
		switch {
		case errors.Is(err, session.ErrNotFound):
			next.ServeHTTP(w, r)
		case err != nil:
			slog.Error("http: load session", "err", err)
			errUnavailable(w)
		default:
			if cfg.Session.Sliding && !now.Truncate(time.Hour).Equal(prevSeen.Truncate(time.Hour)) {
				setCookie(w, cfg, SessionCookie, sess.ID, time.Duration(cfg.Session.DurationHours)*time.Hour)
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keySession, &sess)))
		}
	})
}

// requireSession rejects requests without a valid session with 401.
func requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Je bent niet ingelogd.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// parsePrefixes parses CIDRs, skipping invalid entries (settings validation
// rejects them, so none are expected).
func parsePrefixes(list []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(list))
	for _, c := range list {
		if p, err := netip.ParsePrefix(c); err == nil {
			out = append(out, p.Masked())
		}
	}
	return out
}

func containsIP(prefixes []netip.Prefix, ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// requireSessionOrLogin sends browsers without a session to the sign-in page,
// which returns them here afterwards (web: safeNext).
func requireSessionOrLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()) == nil {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
