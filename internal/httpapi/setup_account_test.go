package httpapi

import (
	"context"
	"testing"
	"time"
)

// The short setup: setup code, then a Keystone account; everything else
// (domain, AD, passkey, emergency account) is set later in Instellingen.
func TestSetupLocalAccount(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.10")

	c.must("POST", "/api/setup/account", map[string]string{"username": "thijs", "password": "long enough secret"}, 401) // not unlocked
	s.unlock(c)
	c.must("POST", "/api/setup/account", map[string]string{"username": "thijs", "password": "short"}, 400)
	c.must("POST", "/api/setup/account", map[string]string{"username": "th ijs", "password": "long enough secret"}, 400)
	c.must("POST", "/api/setup/account", map[string]string{"username": "Thijs", "password": "long enough secret"}, 200)

	if needed, _ := status(t, c); needed {
		t.Fatal("setup still needed")
	}
	var me map[string]any
	body(t, c.must("GET", "/api/me", nil, 200).Body, &me)
	if me["username"] != "thijs" || me["passkey_missing"] != true {
		t.Fatalf("me = %v", me)
	}
	var cfg map[string]any
	body(t, c.must("GET", "/api/admin/settings", nil, 200).Body, &cfg)
	if cfg["public_url"] != setupOrigin {
		t.Fatalf("public_url = %v, want the address setup ran on", cfg["public_url"])
	}
	c.must("POST", "/api/setup/account", map[string]string{"username": "eve", "password": "long enough secret"}, 410) // done

	// Signing in later checks the Keystone password; AD is not needed.
	s.deps.Dir = nil
	s.h = New(s.deps)
	c2 := newClient(t, s.env, "192.168.1.11")
	if r := password(c2, "thijs", "wrong password!!"); r.Code != 401 {
		t.Fatalf("wrong password = %d %v", r.Code, r.Body)
	}
	if r := password(c2, "eve", "long enough secret"); r.Code != 401 {
		t.Fatalf("other user = %d %v", r.Code, r.Body)
	}
	if r := password(c2, "THIJS", "long enough secret"); r.Code != 200 || r.Body["next"] != "done" {
		t.Fatalf("password login = %d %v", r.Code, r.Body)
	}
}

// An earlier, unfinished wizard may have saved an https public address the
// browser is not on. The account step makes the address in use the public
// one, so the session cookie sticks and the next sign-in is accepted.
func TestSetupAccountOverridesEarlierAddress(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.10")
	s.unlock(c)
	s.domain(c) // saves public_url https://keystone.home.lan

	c.host, c.origin = "192.168.1.10:8080", "http://192.168.1.10:8080"
	s.unlock(c)
	w := c.must("POST", "/api/setup/account", map[string]string{"username": "thijs", "password": "long enough secret"}, 200)
	for _, ck := range w.Result().Cookies() {
		if ck.Name == SessionCookie && ck.Secure {
			t.Fatal("session cookie is Secure on an http address: the browser drops it")
		}
	}
	c.must("GET", "/api/me", nil, 200)
	c.must("POST", "/api/auth/logout", nil, 200)

	c2 := newClient(t, s.env, "192.168.1.11")
	c2.host, c2.origin = "192.168.1.10:8080", "http://192.168.1.10:8080"
	if r := password(c2, "thijs", "long enough secret"); r.Code != 200 || r.Body["next"] != "done" {
		t.Fatalf("sign-in on the setup address = %d %v", r.Code, r.Body)
	}
}

// Keystone opened on another address than the one setup ran on — the
// server's IP, a new domain behind a proxy — still signs in, with a cookie
// the browser keeps there. A page on a foreign origin is still refused.
func TestSignInOnAnotherAddress(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.10")
	s.unlock(c)
	c.must("POST", "/api/setup/account", map[string]string{"username": "thijs", "password": "long enough secret"}, 200)
	s.deps.Dir = nil
	s.h = New(s.deps)

	for _, tc := range []struct {
		host, origin string
		secure       bool
	}{
		{"192.168.1.5:8080", "http://192.168.1.5:8080", false},
		{"auth.home.lan", "https://auth.home.lan", true},
	} {
		b := newClient(t, s.env, "192.168.1.20")
		b.host, b.origin = tc.host, tc.origin
		w := b.must("POST", "/api/auth/password", map[string]string{"username": "thijs", "password": "long enough secret"}, 200)
		var session bool
		for _, ck := range w.Result().Cookies() {
			if ck.Name == SessionCookie {
				session = true
				if ck.Secure != tc.secure {
					t.Fatalf("%s: session cookie Secure = %v", tc.origin, ck.Secure)
				}
			}
		}
		if !session {
			t.Fatalf("%s: no session cookie", tc.origin)
		}
		b.must("GET", "/api/me", nil, 200)
	}

	evil := newClient(t, s.env, "192.168.1.21")
	evil.host, evil.origin = "192.168.1.5:8080", "http://evil.example"
	if w := evil.do("POST", "/api/auth/password", map[string]string{"username": "thijs", "password": "long enough secret"}); w.Code != 403 || errCode(t, w) != "bad_origin" {
		t.Fatalf("foreign origin = %d %s", w.Code, w.Body.String())
	}
}

// Passkeys from an abandoned run of the old wizard must not block the
// account step: setup never finished, so nobody can be using them.
func TestSetupAccountAfterAbandonedPasskey(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.10")
	s.unlock(c)
	s.domain(c)
	s.ad(c)
	s.owner(c)
	s.passkey(c)
	c.must("POST", "/api/setup/account", map[string]string{"username": "thijs", "password": "long enough secret"}, 200)
	var me map[string]any
	body(t, c.must("GET", "/api/me", nil, 200).Body, &me)
	if me["passkey_missing"] != true {
		t.Fatalf("passkeys of the abandoned setup kept: %v", me)
	}
}

// After setup is finished the account step is closed: it never replaces the
// owner, the password or the passkeys of a running Keystone.
func TestSetupAccountClosedAfterSetup(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t) // finished setup with an AD owner and a passkey
	c := newClient(t, l.env, "192.168.1.30")
	c.must("POST", "/api/setup/unlock", map[string]string{"code": l.code}, 410)
	c.cookies[PendingCookie] = "setup.forged"
	c.must("POST", "/api/setup/account", map[string]string{"username": "eve", "password": "long enough secret"}, 410)
	var passkeys int
	var owner string
	if err := l.deps.Pool.QueryRow(context.Background(), `select (select count(*) from passkeys), (select username from owner)`).Scan(&passkeys, &owner); err != nil {
		t.Fatal(err)
	}
	if passkeys == 0 || owner != "thijs" {
		t.Fatalf("passkeys=%d owner=%q after a closed account step", passkeys, owner)
	}
}

// Without a passkey, a sensitive action is confirmed with the password; the
// vault's own reconfirm window follows the same rule.
func TestStepUpWithPassword(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.10")
	s.unlock(c)
	c.must("POST", "/api/setup/account", map[string]string{"username": "thijs", "password": "long enough secret"}, 200)
	ctx := context.Background()
	cfg, err := s.deps.Settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.deps.Sessions.Get(ctx, c.cookies[SessionCookie], cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sess.LastFullAuthAt = time.Now().Add(-time.Hour)
	if err := s.deps.Sessions.Update(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if w := c.do("PUT", "/api/admin/branding", map[string]string{"name": "Huis", "accent": "#007aff", "background": "#ffffff"}); w.Code != 403 || errCode(t, w) != "stepup_required" {
		t.Fatalf("stale session = %d %s", w.Code, w.Body.String())
	}
	if w := c.do("POST", "/api/auth/stepup/password", map[string]string{"password": "wrong password!!"}); w.Code != 401 {
		t.Fatalf("wrong password = %d %s", w.Code, w.Body.String())
	}
	c.must("POST", "/api/auth/stepup/password", map[string]string{"password": "long enough secret"}, 200)
	c.must("PUT", "/api/admin/branding", map[string]string{"name": "Huis", "accent": "#007aff", "background": "#ffffff"}, 200)
}
