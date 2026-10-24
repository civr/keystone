package httpapi

import (
	"strings"
	"testing"

	"github.com/civr/keystone/internal/settings"
)

// Admins add accounts; a member signs in with a Keystone password, sees only
// the apps meant for them and keeps a private vault, and cannot change
// settings. Disabling an account signs it out.
func TestAccounts(t *testing.T) {
	t.Parallel()
	l, owner := adminClient(t, "192.168.1.20")
	l.settings(t, func(s *settings.Settings) { s.Login.PasskeyRequired, s.BruteForce.LoginPerAccountPerMinute = false, 100 })

	owner.must("POST", "/api/admin/users", map[string]any{"username": "Thijs", "password": "long enough secret", "role": "member"}, 400) // the owner's name
	owner.must("POST", "/api/admin/users", map[string]any{"username": "anna", "password": "short", "role": "member"}, 400)
	owner.must("POST", "/api/admin/users", map[string]any{"username": "anna", "password": "long enough secret", "role": "boss"}, 400)
	var anna userView
	body(t, owner.must("POST", "/api/admin/users", map[string]any{
		"username": "Anna", "display_name": "Anna de Vries", "email": "anna@home.lan", "password": "long enough secret", "role": "member",
	}, 201).Body, &anna)
	if anna.Username != "anna" || anna.Role != "member" {
		t.Fatalf("created = %+v", anna)
	}
	owner.must("POST", "/api/admin/users", map[string]any{"username": "anna", "password": "long enough secret", "role": "member"}, 400)

	// One app for everyone, one for the owner only.
	var open, private map[string]any
	body(t, owner.must("POST", "/api/apps", map[string]any{"name": "Jellyfin", "url": "https://jf.home.lan", "color": "#00a4dc"}, 201).Body, &open)
	body(t, owner.must("POST", "/api/apps", map[string]any{"name": "Proxmox", "url": "https://pve.home.lan", "color": "#e57000", "access": []string{"owner"}}, 201).Body, &private)
	owner.must("POST", "/api/apps", map[string]any{"name": "X", "url": "https://x.home.lan", "color": "#e57000", "access": []string{"nobody"}}, 400)
	owner.must("POST", "/api/vault", map[string]any{"title": "Owner's router"}, 201)

	a := newClient(t, l.env, "192.168.1.30")
	if r := password(a, "anna", "wrong password!!"); r.Code != 401 {
		t.Fatalf("wrong password = %d", r.Code)
	}
	if r := password(a, "ANNA", "long enough secret"); r.Code != 200 || r.Body["next"] != "done" {
		t.Fatalf("sign-in = %d %v", r.Code, r.Body)
	}
	m := me(a)
	if m["username"] != "anna" || m["role"] != "member" || m["admin"] != false {
		t.Fatalf("me = %v", m)
	}
	apps := listApps(t, a)
	if len(apps) != 1 || apps[0].Name != "Jellyfin" {
		t.Fatalf("member apps = %+v", apps)
	}
	if len(listApps(t, owner)) != 2 {
		t.Fatal("admin sees every app")
	}
	a.must("GET", "/api/admin/settings", nil, 403)
	a.must("GET", "/api/admin/users", nil, 403)
	a.must("POST", "/api/apps", map[string]any{"name": "Y", "url": "https://y.home.lan", "color": "#e57000"}, 403)
	a.must("GET", "/api/admin/emergency", nil, 403)
	a.must("GET", "/api/admin/passkeys", nil, 200)
	a.must("GET", "/api/admin/sessions", nil, 200)

	if strings.Contains(a.must("GET", "/api/vault", nil, 200).Body.String(), "router") {
		t.Fatal("a member sees the owner's vault")
	}
	a.must("POST", "/api/vault", map[string]any{"title": "Anna's mail"}, 201)
	if strings.Contains(owner.must("GET", "/api/vault", nil, 200).Body.String(), "mail") {
		t.Fatal("the owner sees a member's vault")
	}

	// An admin account may manage; a member account may not become owner-only.
	a.must("PUT", "/api/account/password", map[string]string{"current": "nope", "new": "another long secret"}, 400)
	a.must("PUT", "/api/account/password", map[string]string{"current": "long enough secret", "new": "another long secret"}, 200)
	if r := password(newClient(t, l.env, "192.168.1.31"), "anna", "another long secret"); r.Code != 200 {
		t.Fatalf("new password = %d", r.Code)
	}

	disabled := true
	owner.must("PUT", "/api/admin/users/"+anna.ID, map[string]any{"disabled": disabled}, 200)
	a.must("GET", "/api/me", nil, 401)
	if r := password(newClient(t, l.env, "192.168.1.32"), "anna", "another long secret"); r.Code != 401 {
		t.Fatalf("disabled sign-in = %d", r.Code)
	}
	owner.must("PUT", "/api/admin/users/owner", map[string]any{"role": "member"}, 403)
	owner.must("DELETE", "/api/admin/users/"+anna.ID, nil, 200)
	var list struct{ Users []userView }
	body(t, owner.must("GET", "/api/admin/users", nil, 200).Body, &list)
	if len(list.Users) != 1 || list.Users[0].Role != "owner" {
		t.Fatalf("users = %+v", list.Users)
	}
	if l.audits(t, "user.created") != 1 || l.audits(t, "user.deleted") != 1 {
		t.Fatal("audit")
	}
}

// The owner's name at an app (an account override) cannot become another
// account's username, either way round: that person would be the owner there.
func TestOwnerAppNameReserved(t *testing.T) {
	t.Parallel()
	_, owner := adminClient(t, "192.168.1.20")
	owner.must("POST", "/api/apps", map[string]any{"name": "Jellyfin", "url": "https://jf.home.lan", "color": "#00a4dc",
		"config": map[string]any{"account": map[string]string{"username": "admin"}}}, 201)
	owner.must("POST", "/api/admin/users", map[string]any{"username": "Admin", "password": "long enough secret", "role": "member"}, 400)
	owner.must("POST", "/api/admin/users", map[string]any{"username": "bob", "password": "long enough secret", "role": "member"}, 201)
	if w := owner.do("POST", "/api/apps", map[string]any{"name": "Grafana", "url": "https://gf.home.lan", "color": "#00a4dc",
		"config": map[string]any{"account": map[string]string{"username": "BOB"}}}); w.Code != 400 || !strings.Contains(w.Body.String(), "config.account.username") {
		t.Fatalf("override = %d %s", w.Code, w.Body.String())
	}
}
