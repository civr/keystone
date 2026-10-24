package httpapi

import (
	"context"
	"testing"
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
