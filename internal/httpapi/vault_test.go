package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/civr/keystone/internal/settings"
)

func TestVault(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	var a protoApp
	body(t, c.must("POST", "/api/apps", map[string]any{"name": "UniFi", "url": "https://unifi.home.lan", "color": "#006FFF", "protocol": "vault"}, 201).Body, &a)
	base := "/api/apps/" + a.ID + "/vault"

	t.Run("RoundTrip", func(t *testing.T) {
		c.must("PUT", base, map[string]any{"username": "admin", "password": "s3cret!"}, 200)
		var v struct {
			Username    string `json:"username"`
			HasPassword bool   `json:"has_password"`
		}
		w := c.must("GET", base, nil, 200)
		body(t, w.Body, &v)
		if v.Username != "admin" || !v.HasPassword || strings.Contains(w.Body.String(), "s3cret") {
			t.Fatalf("get = %s", w.Body.String())
		}
		var p struct {
			Password string `json:"password"`
		}
		body(t, c.must("POST", base+"/password", nil, 200).Body, &p)
		if p.Password != "s3cret!" {
			t.Fatalf("password = %q", p.Password)
		}
		// Omitted password keeps it.
		c.must("PUT", base, map[string]any{"username": "root"}, 200)
		body(t, c.must("POST", base+"/password", nil, 200).Body, &p)
		if p.Password != "s3cret!" {
			t.Fatal("password lost on username change")
		}
		if l.audits(t, "vault.revealed") < 2 || l.audits(t, "vault.updated") < 2 {
			t.Fatal("audit missing")
		}
	})

	t.Run("SealedAtRest", func(t *testing.T) {
		var sealed string
		if err := l.deps.Pool.QueryRow(context.Background(), `select password_sealed from vault_entries where app_id = $1`, a.ID).Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(sealed, "s3cret") {
			t.Fatal("password stored in clear")
		}
	})

	t.Run("PasswordNeedsRecentPasskey", func(t *testing.T) {
		// The default window is 0 (any sign-in opens the vault); test a set one.
		l.put(t, func(s *settings.Settings) { s.Vault.ReconfirmMinutes = 15 })
		defer l.put(t, func(s *settings.Settings) { s.Vault.ReconfirmMinutes = 0 })
		sess := l.session(t, c)
		sess.LastPasskeyAt = time.Now().Add(-16 * time.Minute)
		if err := l.deps.Sessions.Update(context.Background(), sess); err != nil {
			t.Fatal(err)
		}
		w := c.do("POST", base+"/password", nil)
		if w.Code != 403 || errCode(t, w) != "stepup_required" || strings.Contains(w.Body.String(), "s3cret") {
			t.Fatalf("got %d %s", w.Code, w.Body.String())
		}
		sess.LastPasskeyAt = time.Now()
		_ = l.deps.Sessions.Update(context.Background(), sess)
	})

	t.Run("OnlyForVaultApps", func(t *testing.T) {
		var link protoApp
		body(t, c.must("POST", "/api/apps", map[string]any{"name": "L", "url": "https://l.lan", "color": "#000000"}, 201).Body, &link)
		c.must("PUT", "/api/apps/"+link.ID+"/vault", map[string]any{"username": "x", "password": "y"}, 400)
	})

	t.Run("ClearedWhenProtocolChanges", func(t *testing.T) {
		var other protoApp
		body(t, c.must("POST", "/api/apps", map[string]any{"name": "NAS", "url": "https://nas.lan", "color": "#111111", "protocol": "vault"}, 201).Body, &other)
		c.must("PUT", "/api/apps/"+other.ID+"/vault", map[string]any{"username": "admin", "password": "pw"}, 200)
		c.must("PUT", "/api/apps/"+other.ID, map[string]any{"name": "NAS", "url": "https://nas.lan", "color": "#111111", "protocol": "link"}, 200)
		var n int
		_ = l.deps.Pool.QueryRow(context.Background(), `select count(*) from vault_entries where app_id = $1`, other.ID).Scan(&n)
		if n != 0 {
			t.Fatal("vault entry kept after the app stopped using the vault")
		}
	})

	t.Run("DeletedWithApp", func(t *testing.T) {
		c.must("DELETE", "/api/apps/"+a.ID, nil, 200)
		var n int
		_ = l.deps.Pool.QueryRow(context.Background(), `select count(*) from vault_entries`).Scan(&n)
		if n != 0 {
			t.Fatalf("%d entries left", n)
		}
	})
}
