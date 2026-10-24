package httpapi

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/civr/keystone/internal/gatetoken"
	"github.com/civr/keystone/internal/settings"
)

func TestGateStart(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	c.must("POST", "/api/apps", map[string]any{
		"name": "Home Assistant", "url": "https://ha.home.lan", "color": "#18BCF2", "protocol": "gate",
		"config": map[string]any{"gate": map[string]any{"host": "ha.home.lan", "bypass_paths": []string{"/api/"}}},
	}, 201)
	start := func(rd string) string { return "/api/gate/start?rd=" + url.QueryEscape(rd) }

	t.Run("SetsDomainCookie", func(t *testing.T) {
		w := c.must("GET", start("https://ha.home.lan/lovelace/0"), nil, 302)
		if w.Header().Get("Location") != "https://ha.home.lan/lovelace/0" {
			t.Fatalf("location = %q", w.Header().Get("Location"))
		}
		var tok string
		for _, ck := range w.Result().Cookies() {
			if ck.Name == gatetoken.Cookie {
				if ck.Domain != "home.lan" || !ck.HttpOnly || !ck.Secure {
					t.Fatalf("cookie = %+v", ck)
				}
				tok = ck.Value
			}
		}
		sess, err := gatetoken.Resolve(context.Background(), l.deps.Valkey, l.deps.Sessions, settings.Defaults(), tok, time.Now())
		if err != nil || sess.ID != c.cookies[SessionCookie] {
			t.Fatalf("resolve: %v", err)
		}
	})

	t.Run("RejectsForeignHost", func(t *testing.T) {
		for _, rd := range []string{"https://evil.com/", "https://home.lan.evil.com/", "https://other.home.lan/", "javascript:alert(1)", "//ha.home.lan/", "https://ha.home.lan@evil.com/"} {
			if w := c.do("GET", start(rd), nil); w.Code != 400 {
				t.Errorf("%s → %d %s", rd, w.Code, w.Header().Get("Location"))
			}
		}
	})

	t.Run("HTTPAppRefusedWithSecureCookie", func(t *testing.T) {
		// public_url is https, so ks_gate is Secure: a browser on http://
		// never sends it back and would loop between the gate and Keystone.
		w := c.do("GET", start("http://ha.home.lan/"), nil)
		if w.Code != 400 || w.Header().Get("Location") != "" {
			t.Fatalf("http rd = %d %q", w.Code, w.Header().Get("Location"))
		}
	})

	t.Run("NoSessionGoesToLogin", func(t *testing.T) {
		anon := newClient(t, l.env, "192.168.1.23")
		w := anon.must("GET", start("https://ha.home.lan/"), nil, 302)
		if !strings.HasPrefix(w.Header().Get("Location"), "/login?next=%2Fapi%2Fgate%2Fstart") {
			t.Fatalf("location = %q", w.Header().Get("Location"))
		}
	})

	t.Run("LogoutRevokesGateToken", func(t *testing.T) {
		w := c.must("GET", start("https://ha.home.lan/"), nil, 302)
		var tok string
		for _, ck := range w.Result().Cookies() {
			if ck.Name == gatetoken.Cookie {
				tok = ck.Value
			}
		}
		c.must("POST", "/api/auth/logout", nil, 200)
		if _, err := gatetoken.Resolve(context.Background(), l.deps.Valkey, l.deps.Sessions, settings.Defaults(), tok, time.Now()); err != gatetoken.ErrNotFound {
			t.Fatalf("after logout = %v", err)
		}
	})
}
