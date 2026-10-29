package httpapi

import (
	"strings"
	"testing"

	"github.com/civr/keystone/internal/settings"
)

// signedIn adds an account and returns a client signed in as it.
func signedIn(t *testing.T, l *loginEnv, owner *client, name, ip string) (*client, string) {
	t.Helper()
	var u userView
	body(t, owner.must("POST", "/api/admin/users", map[string]any{"username": name, "password": "long enough secret"}, 201).Body, &u)
	c := newClient(t, l.env, ip)
	if r := password(c, name, "long enough secret"); r.Code != 200 {
		t.Fatalf("sign-in %s = %d %v", name, r.Code, r.Body)
	}
	return c, u.ID
}

func setRoles(t *testing.T, c *client, id string, want int, roles ...map[string]string) {
	t.Helper()
	c.must("PUT", "/api/admin/users/"+id+"/roles", map[string]any{"roles": roles}, want)
}

// Roles open exactly what they name, on the server; nobody hands out or takes
// over more than they hold, directly, through groups, or by resetting a
// stronger account's password.
func TestRBAC(t *testing.T) {
	t.Parallel()
	l, owner := adminClient(t, "192.168.1.20")
	l.settings(t, func(s *settings.Settings) {
		s.Login.PasskeyRequired, s.BruteForce.LoginPerAccountPerMinute = false, 100
	})

	member, _ := signedIn(t, l, owner, "mia", "192.168.1.30")
	auditor, auditorID := signedIn(t, l, owner, "aud", "192.168.1.31")
	admin, adminID := signedIn(t, l, owner, "adm", "192.168.1.32")
	roleman, rolemanID := signedIn(t, l, owner, "rol", "192.168.1.33")

	for _, p := range []string{"/api/admin/users", "/api/admin/roles", "/api/admin/groups", "/api/admin/audit", "/api/admin/settings"} {
		member.must("GET", p, nil, 403)
	}

	setRoles(t, owner, auditorID, 200, map[string]string{"role": "auditor"})
	auditor.must("GET", "/api/admin/users", nil, 200)
	auditor.must("GET", "/api/admin/audit", nil, 200)
	auditor.must("POST", "/api/admin/users", map[string]any{"username": "x1", "password": "long enough secret"}, 403)
	auditor.must("GET", "/api/admin/settings", nil, 403)

	setRoles(t, owner, adminID, 200, map[string]string{"role": "admin"})
	admin.must("POST", "/api/admin/users", map[string]any{"username": "x2", "password": "long enough secret"}, 201)
	setRoles(t, admin, auditorID, 403) // no roles.write
	admin.must("POST", "/api/admin/roles", map[string]any{"name": "Mine", "permissions": []string{"users.read"}}, 403)
	admin.must("PUT", "/api/admin/users/owner", map[string]any{"password": "taken over account"}, 403)

	// A custom role that may hand out roles, but only what it holds itself.
	var custom roleView
	body(t, owner.must("POST", "/api/admin/roles", map[string]any{"name": "Role manager", "permissions": []string{"roles.write", "users.read"}}, 201).Body, &custom)
	setRoles(t, owner, rolemanID, 200, map[string]string{"role": custom.ID})
	setRoles(t, roleman, rolemanID, 403, map[string]string{"role": custom.ID}, map[string]string{"role": "superadmin"})
	setRoles(t, roleman, rolemanID, 403, map[string]string{"role": custom.ID}, map[string]string{"role": "admin"})
	roleman.must("POST", "/api/admin/roles", map[string]any{"name": "Bigger", "permissions": []string{"users.write"}}, 403)
	// Nor take away what it lacks, nor manage a stronger account.
	setRoles(t, roleman, adminID, 403)
	roleman.must("PUT", "/api/admin/users/"+adminID, map[string]any{"password": "taken over account"}, 403)

	// Groups: membership hands out the group's roles.
	var grp map[string]string
	body(t, owner.must("POST", "/api/admin/groups", map[string]any{"name": "Root"}, 201).Body, &grp)
	owner.must("PUT", "/api/admin/groups/"+grp["id"]+"/roles", map[string]any{"roles": []map[string]string{{"role": "superadmin"}}}, 200)
	admin.must("PUT", "/api/admin/groups/"+grp["id"], map[string]any{"name": "Root", "members": []string{adminID}}, 403)
	admin.must("DELETE", "/api/admin/groups/"+grp["id"], nil, 403)
	if strings.Contains(me(admin)["permissions"].([]any)[0].(string), "*") {
		t.Fatal("admin became super admin")
	}
}

// An application admin changes the apps it is given and nothing else; app
// access can name groups.
func TestAppAdminAndGroupAccess(t *testing.T) {
	t.Parallel()
	l, owner := adminClient(t, "192.168.1.20")
	l.settings(t, func(s *settings.Settings) {
		s.Login.PasskeyRequired, s.BruteForce.LoginPerAccountPerMinute = false, 100
	})
	var a, b map[string]any
	body(t, owner.must("POST", "/api/apps", map[string]any{"name": "A", "url": "https://a.home.lan", "color": "#007aff"}, 201).Body, &a)
	body(t, owner.must("POST", "/api/apps", map[string]any{"name": "B", "url": "https://b.home.lan", "color": "#007aff"}, 201).Body, &b)

	appadm, id := signedIn(t, l, owner, "apa", "192.168.1.40")
	setRoles(t, owner, id, 200, map[string]string{"role": "app_admin", "app": a["id"].(string)})
	appadm.must("PUT", "/api/apps/"+a["id"].(string), map[string]any{"name": "A2", "url": "https://a.home.lan", "color": "#007aff"}, 200)
	// An app admin cannot change who has access or where the vault logs in.
	owner.must("PUT", "/api/apps/"+a["id"].(string), map[string]any{"name": "A", "url": "https://a.home.lan", "color": "#007aff",
		"protocol": "vault", "access": []string{"owner"}, "config": map[string]any{"vault": map[string]any{"mode": "form", "login_url": "https://good.example"}}}, 200)
	var kept struct {
		Access []string
		Config struct {
			Vault struct {
				LoginURL string `json:"login_url"`
			}
		}
	}
	body(t, appadm.must("PUT", "/api/apps/"+a["id"].(string), map[string]any{"name": "A2", "url": "https://a.home.lan", "color": "#007aff",
		"protocol": "vault", "config": map[string]any{"vault": map[string]any{"mode": "form", "login_url": "https://evil.example"}}}, 200).Body, &kept)
	if len(kept.Access) != 1 || kept.Access[0] != "owner" || kept.Config.Vault.LoginURL != "https://good.example" {
		t.Fatalf("app admin changed protected fields: %+v", kept)
	}
	appadm.must("PUT", "/api/apps/"+b["id"].(string), map[string]any{"name": "B2", "url": "https://b.home.lan", "color": "#007aff"}, 403)
	appadm.must("POST", "/api/apps", map[string]any{"name": "C", "url": "https://c.home.lan", "color": "#007aff"}, 403)
	appadm.must("DELETE", "/api/apps/"+a["id"].(string), nil, 403)

	member, mid := signedIn(t, l, owner, "gm", "192.168.1.41")
	var grp map[string]string
	body(t, owner.must("POST", "/api/admin/groups", map[string]any{"name": "Family", "members": []string{mid}}, 201).Body, &grp)
	owner.must("PUT", "/api/apps/"+b["id"].(string), map[string]any{"name": "B", "url": "https://b.home.lan", "color": "#007aff",
		"access": []string{"group:" + grp["id"]}}, 200)
	owner.must("PUT", "/api/apps/"+a["id"].(string), map[string]any{"name": "A", "url": "https://a.home.lan", "color": "#007aff",
		"access": []string{"owner"}}, 200)
	apps := listApps(t, member)
	if len(apps) != 1 || apps[0].Name != "B" {
		t.Fatalf("member apps = %+v", apps)
	}
}

// Admin-wide sessions need sessions.manage; my access and recently used are
// per account.
func TestPortalEndpoints(t *testing.T) {
	t.Parallel()
	l, owner := adminClient(t, "192.168.1.20")
	l.settings(t, func(s *settings.Settings) {
		s.Login.PasskeyRequired, s.BruteForce.LoginPerAccountPerMinute = false, 100
	})
	member, mid := signedIn(t, l, owner, "pm", "192.168.1.50")
	member.must("GET", "/api/admin/sessions/all", nil, 403)
	var all struct{ Sessions []map[string]any }
	body(t, owner.must("GET", "/api/admin/sessions/all", nil, 200).Body, &all)
	var theirs string
	for _, s := range all.Sessions {
		if s["account"] == mid {
			theirs = s["id"].(string)
		}
	}
	if theirs == "" {
		t.Fatal("member session not listed")
	}
	owner.must("DELETE", "/api/admin/sessions/all/"+theirs, nil, 200)
	member.must("GET", "/api/me", nil, 401)

	member, _ = signedIn(t, l, owner, "pm2", "192.168.1.51")
	var acc map[string]any
	body(t, member.must("GET", "/api/account/access", nil, 200).Body, &acc)
	if len(acc["roles"].([]any)) != 0 {
		t.Fatalf("access = %v", acc)
	}
	var a map[string]any
	body(t, owner.must("POST", "/api/apps", map[string]any{"name": "Wiki", "url": "https://wiki.home.lan", "color": "#007aff"}, 201).Body, &a)
	member.must("POST", "/api/apps/"+a["id"].(string)+"/opened", nil, 204)
	var recent struct{ IDs []string }
	body(t, member.must("GET", "/api/apps/recent", nil, 200).Body, &recent)
	if len(recent.IDs) != 1 || recent.IDs[0] != a["id"] {
		t.Fatalf("recent = %v", recent.IDs)
	}
}
