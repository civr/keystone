package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
	"github.com/civr/keystone/internal/testutil"
)

const testShell = `<!doctype html><html><head><title>Keystone</title><link rel="icon" href="/favicon.svg" type="image/svg+xml" /><script>console.log("a")</script><script src="/x.js"></script></head>` +
	`<body><script type="module">import("/app.js")</script></body></html>`

type env struct {
	deps Deps
	h    http.Handler
}

func newEnv(t *testing.T, withValkey bool) *env {
	t.Helper()
	ctx := context.Background()
	pool, err := db.Open(ctx, testutil.Postgres(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	d := Deps{Pool: pool, Settings: settings.NewStore(pool), Audit: audit.NewWriter(pool, nil), Shell: []byte(testShell)}
	t.Cleanup(d.Audit.Close)
	if withValkey {
		opt, err := redis.ParseURL(testutil.Valkey(t))
		if err != nil {
			t.Fatal(err)
		}
		rdb := redis.NewClient(opt)
		t.Cleanup(func() { _ = rdb.Close() })
		d.Valkey = rdb
		d.Sessions = session.NewStore(rdb)
		d.Limiter = session.NewLimiter(rdb)
	}
	return &env{deps: d, h: New(d)}
}

func (e *env) put(t *testing.T, mut func(*settings.Settings)) {
	t.Helper()
	cfg, err := e.deps.Settings.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mut(&cfg)
	if _, err := e.deps.Settings.Put(context.Background(), cfg, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
}

func do(h http.Handler, method, target, remote string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Error, Message string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", w.Body.String(), err)
	}
	if body.Message == "" {
		t.Fatalf("error body without message: %s", w.Body.String())
	}
	return body.Error
}

func TestCSPHashesInlineScripts(t *testing.T) {
	csp := CSP([]byte(testShell), "")
	h1 := sha256.Sum256([]byte(`console.log("a")`))
	h2 := sha256.Sum256([]byte(`import("/app.js")`))
	want := "default-src 'self'; script-src 'self' 'sha256-" + base64.StdEncoding.EncodeToString(h1[:]) +
		"' 'sha256-" + base64.StdEncoding.EncodeToString(h2[:]) +
		"'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	if csp != want {
		t.Fatalf("CSP\n got %s\nwant %s", csp, want)
	}
	if got := CSP([]byte(testShell), " upgrade-insecure-requests; "); got != want+"; upgrade-insecure-requests" {
		t.Fatalf("CSP with extra = %s", got)
	}
}

func TestHeadersFollowSettings(t *testing.T) {
	t.Parallel()
	e := newEnv(t, false)
	e.put(t, func(s *settings.Settings) { s.PublicURL = "https://keystone.home.lan" })

	w := do(e.h, "GET", "/", "172.18.0.2:1", nil)
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Fatalf("HSTS = %q", got)
	}
	if w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", w.Header())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Security-Policy"), "default-src 'self'; script-src 'self' 'sha256-") {
		t.Fatalf("CSP = %q", w.Header().Get("Content-Security-Policy"))
	}

	e.put(t, func(s *settings.Settings) {
		s.Headers.HSTS = false
		s.Headers.FrameDeny = false
		s.Headers.CSPExtra = "upgrade-insecure-requests"
	})
	w = do(e.h, "GET", "/", "172.18.0.2:1", nil)
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS off but header = %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("frame deny off but X-Frame-Options = %q", got)
	}
	if !strings.HasSuffix(w.Header().Get("Content-Security-Policy"), "; upgrade-insecure-requests") {
		t.Fatalf("CSP extra missing: %q", w.Header().Get("Content-Security-Policy"))
	}

	e.put(t, func(s *settings.Settings) {
		s.PublicURL = "http://keystone.home.lan"
		s.Headers.HSTS = true
		s.Headers.HSTSPreload = true
	})
	if got := do(e.h, "GET", "/", "172.18.0.2:1", nil).Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS on plain http public_url = %q", got)
	}
	e.put(t, func(s *settings.Settings) { s.PublicURL = "https://keystone.home.lan"; s.Headers.HSTSMaxAge = 600 })
	if got := do(e.h, "GET", "/", "172.18.0.2:1", nil).Header().Get("Strict-Transport-Security"); got != "max-age=600; includeSubDomains; preload" {
		t.Fatalf("HSTS = %q", got)
	}
}

func TestAdminCIDRBlocks(t *testing.T) {
	t.Parallel()
	e := newEnv(t, false)
	e.put(t, func(s *settings.Settings) {
		s.Network.AdminCIDRs = []string{"192.168.1.0/24"}
		s.Network.LoginCIDRs = []string{"10.0.0.0/8"}
	})
	cases := []struct {
		method, path, remote, xff string
		blocked                   bool
	}{
		{"GET", "/api/admin/settings", "203.0.113.9:1", "", true},
		{"GET", "/api/admin/settings", "172.18.0.2:1", "203.0.113.9", true},
		{"GET", "/api/admin/settings", "172.18.0.2:1", "192.168.1.20", false},
		{"GET", "/api/%61dmin/settings", "203.0.113.9:1", "", true},
		{"POST", "/api/apps", "203.0.113.9:1", "", true},
		{"GET", "/api/apps", "203.0.113.9:1", "", false},
		{"POST", "/api/auth/password", "192.168.1.20:1", "", true},
		{"POST", "/api/auth/password", "10.1.2.3:1", "", false},
		{"GET", "/api/healthz", "203.0.113.9:1", "", false},
	}
	for _, c := range cases {
		hdr := map[string]string{"Origin": "http://example.com"}
		if c.xff != "" {
			hdr["X-Forwarded-For"] = c.xff
		}
		r := httptest.NewRequest(c.method, "http://example.com"+c.path, nil)
		r.RemoteAddr = c.remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		if blocked := w.Code == http.StatusForbidden; blocked != c.blocked {
			t.Errorf("%s %s from %s/%s: status %d, blocked=%v want %v", c.method, c.path, c.remote, c.xff, w.Code, blocked, c.blocked)
			continue
		}
		if c.blocked {
			if code := errCode(t, w); code != "ip_not_allowed" {
				t.Errorf("%s %s: error code %q", c.method, c.path, code)
			}
		}
	}

	// Empty lists allow everyone.
	e.put(t, func(s *settings.Settings) { s.Network.AdminCIDRs = nil; s.Network.LoginCIDRs = nil })
	if w := do(e.h, "GET", "/api/admin/settings", "203.0.113.9:1", nil); w.Code == http.StatusForbidden {
		t.Fatalf("empty admin_cidrs should allow, got %d", w.Code)
	}
}

func TestOriginMismatchRejected(t *testing.T) {
	t.Parallel()
	e := newEnv(t, false)

	// Setup phase: no public_url, Origin host must equal Host.
	post := func(origin string) int {
		hdr := map[string]string{}
		if origin != "" {
			hdr["Origin"] = origin
		}
		r := httptest.NewRequest("POST", "http://keystone.home.lan/api/setup/unlock", nil)
		r.RemoteAddr = "172.18.0.2:1"
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		if w.Code == http.StatusForbidden && errCode(t, w) != "bad_origin" {
			t.Fatalf("403 with unexpected code: %s", w.Body.String())
		}
		return w.Code
	}
	if c := post("https://keystone.home.lan"); c == http.StatusForbidden {
		t.Fatalf("same-host origin during setup rejected")
	}
	if c := post("https://evil.example"); c != http.StatusForbidden {
		t.Fatalf("foreign origin during setup: %d", c)
	}
	if c := post(""); c != http.StatusForbidden {
		t.Fatalf("missing origin: %d", c)
	}

	e.put(t, func(s *settings.Settings) { s.PublicURL = "https://keystone.home.lan" })
	cases := map[string]bool{ // origin → allowed
		"https://keystone.home.lan":     true,
		"https://KEYSTONE.home.lan:443": true,
		// Same host as the request: still allowed on /api/setup until setup
		// completes, so a wrong public_url cannot lock out the wizard.
		"http://keystone.home.lan":       true,
		"https://keystone.home.lan:8443": false,
		"https://evil.example":           false,
		"null":                           false,
	}
	for origin, ok := range cases {
		if c := post(origin); (c != http.StatusForbidden) != ok {
			t.Errorf("origin %q: status %d, allowed want %v", origin, c, ok)
		}
	}
	// Safe methods need no Origin.
	if w := do(e.h, "GET", "/api/healthz", "172.18.0.2:1", nil); w.Code != http.StatusOK {
		t.Fatalf("GET without origin: %d", w.Code)
	}
}

func TestShellServedForUnknownPath(t *testing.T) {
	t.Parallel()
	e := newEnv(t, false)
	for _, p := range []string{"/", "/instellingen/beveiliging", "/some/deep/page?x=1"} {
		w := do(e.h, "GET", p, "172.18.0.2:1", nil)
		if w.Code != http.StatusOK || w.Body.String() != testShell {
			t.Fatalf("GET %s: %d %q", p, w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Fatalf("content type %q", ct)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("cache control %q", cc)
		}
	}
	w := do(e.h, "GET", "/api/does-not-exist", "172.18.0.2:1", nil)
	if w.Code != http.StatusNotFound || errCode(t, w) != "not_found" {
		t.Fatalf("unknown api path: %d %s", w.Code, w.Body.String())
	}
	if w := do(e.h, "GET", "/api/admin/nope", "172.18.0.2:1", nil); w.Code != http.StatusNotFound || errCode(t, w) != "not_found" {
		t.Fatalf("unknown admin path: %d %s", w.Code, w.Body.String())
	}
}

func TestEmbeddedShellIsDefault(t *testing.T) {
	if !strings.Contains(string(EmbeddedShell), "<script>") {
		t.Fatal("embedded placeholder shell must contain an inline script")
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	e := newEnv(t, false)
	w := do(e.h, "GET", "/api/healthz", "172.18.0.2:1", nil)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("healthz: %d %q", w.Code, w.Body.String())
	}
	e.deps.Pool.Close()
	w = do(e.h, "GET", "/api/healthz", "172.18.0.2:1", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("healthz with closed pool: %d", w.Code)
	}
}

func TestSessionLoaderAndRequireSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t, true)
	s := newServer(e.deps)
	r := chi.NewRouter()
	r.Use(s.middlewares()...)
	r.With(requireSession).Get("/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"id": SessionFrom(r.Context()).ID})
	})
	r.Get("/open", func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()) != nil {
			t.Error("session present without cookie")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if w := do(r, "GET", "/open", "172.18.0.2:1", nil); w.Code != http.StatusNoContent {
		t.Fatalf("open route: %d", w.Code)
	}
	w := do(r, "GET", "/me", "172.18.0.2:1", nil)
	if w.Code != http.StatusUnauthorized || errCode(t, w) != "unauthenticated" {
		t.Fatalf("no cookie: %d %s", w.Code, w.Body.String())
	}
	w = do(r, "GET", "/me", "172.18.0.2:1", map[string]string{"Cookie": "ks_session=bogus"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bogus cookie: %d", w.Code)
	}

	cfg, _ := e.deps.Settings.Get(context.Background())
	sess, err := e.deps.Sessions.Create(context.Background(), session.Session{LastFullAuthAt: time.Now()}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	w = do(r, "GET", "/me", "172.18.0.2:1", map[string]string{"Cookie": "ks_session=" + sess.ID})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), sess.ID) {
		t.Fatalf("valid cookie: %d %s", w.Code, w.Body.String())
	}

	if err := e.deps.Sessions.Revoke(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	w = do(r, "GET", "/me", "172.18.0.2:1", map[string]string{"Cookie": "ks_session=" + sess.ID})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session: %d", w.Code)
	}
}

// awayFromMinuteEdge waits for a fresh minute when fewer than 15 s are left in
// the current one: the rate limiter counts per fixed minute, so a test that
// straddles the boundary would see its counter reset halfway.
func awayFromMinuteEdge(t *testing.T) {
	t.Helper()
	if s := time.Now().Second(); s >= 45 {
		time.Sleep(time.Duration(61-s) * time.Second)
	}
}
