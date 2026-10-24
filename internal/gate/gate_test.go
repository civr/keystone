package gate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/gatetoken"
	"github.com/civr/keystone/internal/runtime"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
	"github.com/civr/keystone/internal/testutil"
)

type fixedView struct{ v runtime.View }

func (f *fixedView) Get(context.Context) (runtime.View, error) { return f.v, nil }

func setup(t *testing.T) (*Server, string, *session.Store, session.Session) {
	t.Helper()
	opt, err := redis.ParseURL(testutil.Valkey(t))
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	cfg := settings.Defaults()
	cfg.Domain, cfg.PublicURL = "home.lan", "https://keystone.home.lan"
	st := session.NewStore(rdb)
	sess, err := st.Create(context.Background(), session.Session{LastFullAuthAt: time.Now()}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := gatetoken.Issue(context.Background(), rdb, sess.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	owner := apps.Owner{ID: "owner", Username: "thijs", DisplayName: "Thijs Willems", Email: "thijs@home.lan"}
	s := &Server{
		View: &fixedView{runtime.View{Settings: cfg,
			Owner:    owner,
			Accounts: []apps.Owner{owner},
			Apps: []apps.App{{ID: "1", Name: "Home Assistant", Protocol: apps.GateP, Config: apps.Config{
				Account: apps.Account{Username: "thijs_ha"},
				Gate:    &apps.Gate{Host: "ha.home.lan", BypassPaths: []string{"/api/", "/auth/token"}}}}}}},
		Valkey: rdb, Sessions: st, Limiter: session.NewLimiter(rdb),
	}
	return s, tok, st, sess
}

func verify(h http.Handler, query, host, uri, cookie string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/verify"+query, nil)
	r.RemoteAddr = "172.18.0.5:3000"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", host)
	r.Header.Set("X-Forwarded-Uri", uri)
	r.Header.Set("X-Forwarded-For", "192.168.1.50")
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: gatetoken.Cookie, Value: cookie})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestVerify(t *testing.T) {
	s, tok, st, sess := setup(t)
	h := s.Handler()

	t.Run("ValidCookieSetsHeaders", func(t *testing.T) {
		w := verify(h, "", "ha.home.lan", "/lovelace", tok)
		if w.Code != 200 || w.Header().Get("X-Keystone-User") != "thijs_ha" ||
			w.Header().Get("X-Keystone-Email") != "thijs@home.lan" || w.Header().Get("X-Keystone-Name") != "Thijs Willems" {
			t.Fatalf("got %d %v", w.Code, w.Header())
		}
	})

	t.Run("NoCookieRedirects", func(t *testing.T) {
		w := verify(h, "", "ha.home.lan", "/lovelace?x=1", "")
		want := "https://keystone.home.lan/api/gate/start?rd=https%3A%2F%2Fha.home.lan%2Flovelace%3Fx%3D1"
		if w.Code != 302 || w.Header().Get("Location") != want {
			t.Fatalf("got %d %q", w.Code, w.Header().Get("Location"))
		}
	})

	t.Run("NginxMode401", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/verify?mode=nginx", nil)
		r.Header.Set("X-Original-URL", "https://ha.home.lan/")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("got %d", w.Code)
		}
		r = httptest.NewRequest("GET", "/login-redirect?rd="+"https%3A%2F%2Fha.home.lan%2F", nil)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 302 || w.Header().Get("Location") != "https://keystone.home.lan/api/gate/start?rd=https%3A%2F%2Fha.home.lan%2F" {
			t.Fatalf("login-redirect = %d %q", w.Code, w.Header().Get("Location"))
		}
	})

	t.Run("BypassPath", func(t *testing.T) {
		w := verify(h, "", "ha.home.lan", "/api/states", "")
		if w.Code != 200 || w.Header().Get("X-Keystone-User") != "" {
			t.Fatalf("got %d %v", w.Code, w.Header())
		}
		if w := verify(h, "", "ha.home.lan", "/apix", ""); w.Code != 302 {
			t.Fatalf("/apix = %d", w.Code)
		}
		if w := verify(h, "", "ha.home.lan", "/api/../lovelace", ""); w.Code != 302 {
			t.Fatalf("dot-dot bypass = %d", w.Code)
		}
	})

	t.Run("SpoofedOriginalURLIgnored", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/verify", nil)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "ha.home.lan")
		r.Header.Set("X-Forwarded-Uri", "/lovelace")
		r.Header.Set("X-Original-URL", "https://ha.home.lan/api/states") // client-supplied, passed on by Traefik
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 302 {
			t.Fatalf("spoofed bypass = %d", w.Code)
		}
	})

	t.Run("UnknownHost403", func(t *testing.T) {
		if w := verify(h, "", "evil.home.lan", "/", tok); w.Code != 403 {
			t.Fatalf("got %d", w.Code)
		}
	})

	t.Run("P95Under5ms", func(t *testing.T) {
		var d []time.Duration
		for range 500 {
			t0 := time.Now()
			if w := verify(h, "", "ha.home.lan", "/", tok); w.Code != 200 {
				t.Fatal(w.Code)
			}
			d = append(d, time.Since(t0))
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		if p95 := d[474]; p95 > 5*time.Millisecond {
			t.Fatalf("p95 = %v", p95)
		}
	})

	t.Run("RevokedSession", func(t *testing.T) {
		_ = st.Revoke(context.Background(), sess.ID)
		if w := verify(h, "", "ha.home.lan", "/", tok); w.Code != 302 {
			t.Fatalf("got %d", w.Code)
		}
	})
}
