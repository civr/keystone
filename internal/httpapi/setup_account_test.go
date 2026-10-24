package httpapi

import (
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
