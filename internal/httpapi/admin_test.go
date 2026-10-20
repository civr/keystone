package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/civr/keystone/internal/passkey/passkeytest"
	"github.com/civr/keystone/internal/settings"
)

// adminClient is a fully signed-in client of a complete Keystone.
func adminClient(t *testing.T, ip string) (*loginEnv, *client) {
	t.Helper()
	l := newLoginEnv(t)
	c := newClient(t, l.env, ip)
	l.fullLogin(c)
	return l, c
}

func body(t *testing.T, w interface{ Bytes() []byte }, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Bytes(), v); err != nil {
		t.Fatalf("decode %s: %v", w.Bytes(), err)
	}
}

func getSettings(t *testing.T, c *client) settings.Settings {
	t.Helper()
	var s settings.Settings
	body(t, c.must("GET", "/api/admin/settings", nil, 200).Body, &s)
	return s
}

func TestSettingsPutRequiresStepUp(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	cur := getSettings(t, c)
	sess := l.session(t, c)
	sess.LastPasskeyAt = time.Now().Add(-2 * time.Hour)
	if err := l.deps.Sessions.Update(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	w := c.do("PUT", "/api/admin/settings", cur)
	if w.Code != http.StatusForbidden || errCode(t, w) != "stepup_required" {
		t.Fatalf("PUT = %d %s", w.Code, w.Body.String())
	}
	c.must("GET", "/api/admin/settings", nil, 200)
	// Without a session: 401.
	if w := newClient(t, l.env, "192.168.1.21").do("GET", "/api/admin/settings", nil); w.Code != 401 {
		t.Fatalf("anonymous GET = %d", w.Code)
	}
}

func TestSettingsPutRejectsSelfLockout(t *testing.T) {
	t.Parallel()
	_, c := adminClient(t, "192.168.1.20")
	cur := getSettings(t, c)
	cur.Network.AdminCIDRs = []string{"10.0.0.0/8"}
	w := c.do("PUT", "/api/admin/settings", cur)
	var res map[string]string
	body(t, w.Body, &res)
	if w.Code != 400 || res["error"] != "invalid" || res["field"] != "network.admin_cidrs" || res["message"] == "" {
		t.Fatalf("PUT = %d %s", w.Code, w.Body.String())
	}
	if got := getSettings(t, c); len(got.Network.AdminCIDRs) != 0 {
		t.Fatalf("lockout settings were stored: %v", got.Network.AdminCIDRs)
	}
}

func TestSettingsChangeAudited(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	cur := getSettings(t, c)
	old := cur.Session.DurationHours
	cur.Session.DurationHours = 24
	cur.Network.TrustedProxies = []string{"172.16.0.0/12"}
	c.must("PUT", "/api/admin/settings", cur, 200)
	if got := getSettings(t, c); got.Session.DurationHours != 24 {
		t.Fatalf("not stored: %+v", got.Session)
	}
	var raw []byte
	if err := l.deps.Pool.QueryRow(context.Background(),
		`select detail from audit_log where action = 'settings.changed' order by id desc limit 1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var d struct {
		Changes []struct {
			Path     string `json:"path"`
			Old, New any
		} `json:"changes"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, ch := range d.Changes {
		found[ch.Path] = true
		if ch.Path == "session.duration_hours" && (ch.Old != float64(old) || ch.New != float64(24)) {
			t.Fatalf("change = %+v", ch)
		}
	}
	if len(d.Changes) != 2 || !found["session.duration_hours"] || !found["network.trusted_proxies"] {
		t.Fatalf("changes = %s", raw)
	}
	// No-op save writes no audit entry.
	c.must("PUT", "/api/admin/settings", getSettings(t, c), 200)
	if l.audits(t, "settings.changed") != 1 {
		t.Fatal("no-op save was audited")
	}
}

func TestSettingsPublicURLHostChangeNeedsConfirm(t *testing.T) {
	t.Parallel()
	_, c := adminClient(t, "192.168.1.20")
	cur := getSettings(t, c)
	cur.PublicURL = "https://other.home.lan"
	w := c.do("PUT", "/api/admin/settings", cur)
	var res map[string]string
	body(t, w.Body, &res)
	if w.Code != 409 || res["error"] != "confirm_required" || res["warning"] != "passkeys_rp_change" {
		t.Fatalf("PUT = %d %s", w.Code, w.Body.String())
	}
	if getSettings(t, c).PublicURL != setupOrigin {
		t.Fatal("public_url changed without confirmation")
	}
	// Only the case differs: same host, no warning needed.
	same := cur
	same.PublicURL = "https://Keystone.home.lan"
	c.must("PUT", "/api/admin/settings", same, 200)
	// Confirmed host change goes through (the Origin then no longer matches for later calls).
	c.must("PUT", "/api/admin/settings?confirm=1", cur, 200)
}

func TestADGetHidesPassword(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	var res map[string]any
	body(t, c.must("GET", "/api/admin/ad", nil, 200).Body, &res)
	if res["bind_password_set"] != true || res["bind_dn"] != "cn=svc,dc=home,dc=lan" {
		t.Fatalf("GET = %v", res)
	}
	for k := range res {
		if strings.Contains(k, "sealed") || k == "bind_password" {
			t.Fatalf("GET leaks %q", k)
		}
	}
	sealed := func() string {
		var s string
		if err := l.deps.Pool.QueryRow(context.Background(), `select data->>'bind_password_sealed' from ad_config`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := sealed()
	// Omitted password keeps the sealed one.
	upd := map[string]any{"url": "ldaps://dc2.home.lan", "bind_dn": "cn=svc,dc=home,dc=lan", "base_dn": "dc=home,dc=lan"}
	c.must("PUT", "/api/admin/ad", upd, 200)
	if sealed() != before {
		t.Fatal("bind password re-sealed without a new one")
	}
	body(t, c.must("GET", "/api/admin/ad", nil, 200).Body, &res)
	if res["url"] != "ldaps://dc2.home.lan" {
		t.Fatalf("url = %v", res["url"])
	}
	// A new password replaces it.
	upd["bind_password"] = "new-secret"
	c.must("PUT", "/api/admin/ad", upd, 200)
	if sealed() == before {
		t.Fatal("bind password not replaced")
	}
	// Invalid config → 400.
	if w := c.do("PUT", "/api/admin/ad", map[string]any{"url": "http://x", "bind_dn": "a", "base_dn": "b"}); w.Code != 400 {
		t.Fatalf("invalid PUT = %d", w.Code)
	}
	// Test endpoint works with the stored password.
	var tr map[string]any
	body(t, c.must("POST", "/api/admin/ad/test", upd, 200).Body, &tr)
	delete(upd, "bind_password")
	body(t, c.must("POST", "/api/admin/ad/test", upd, 200).Body, &tr)
	if tr["ok"] != true || tr["users_found"] != float64(42) {
		t.Fatalf("test = %v", tr)
	}
}

func addPasskey(t *testing.T, c *client, name string) *passkeytest.TestAuthenticator {
	t.Helper()
	opts := c.must("POST", "/api/admin/passkeys/begin", nil, 200).Body.Bytes()
	a := passkeytest.NewTestAuthenticator(setupOrigin)
	cred, err := a.Register(opts)
	if err != nil {
		t.Fatal(err)
	}
	c.must("POST", "/api/admin/passkeys/finish?name="+name, cred, 200)
	return a
}

type pkItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
}

func listPasskeys(t *testing.T, c *client) []pkItem {
	t.Helper()
	var res struct {
		Passkeys []pkItem `json:"passkeys"`
	}
	body(t, c.must("GET", "/api/admin/passkeys", nil, 200).Body, &res)
	return res.Passkeys
}

func TestCannotDeleteLastPasskey(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	pks := listPasskeys(t, c)
	if len(pks) != 1 || pks[0].Name != "MacBook" || pks[0].LastUsedAt == nil {
		t.Fatalf("passkeys = %+v", pks)
	}
	w := c.do("DELETE", "/api/admin/passkeys/"+pks[0].ID, nil)
	if w.Code != 409 || errCode(t, w) != "last_passkey" {
		t.Fatalf("delete last = %d %s", w.Code, w.Body.String())
	}
	addPasskey(t, c, "Telefoon")
	pks = listPasskeys(t, c)
	if len(pks) != 2 {
		t.Fatalf("after add: %+v", pks)
	}
	c.must("PATCH", "/api/admin/passkeys/"+pks[0].ID, map[string]string{"name": "Laptop"}, 200)
	if c.do("PATCH", "/api/admin/passkeys/"+pks[0].ID, map[string]string{"name": " "}).Code != 400 {
		t.Fatal("blank name accepted")
	}
	if c.do("PATCH", "/api/admin/passkeys/AAAA", map[string]string{"name": "x"}).Code != 404 {
		t.Fatal("unknown passkey not 404")
	}
	c.must("DELETE", "/api/admin/passkeys/"+pks[0].ID, nil, 200)
	rest := listPasskeys(t, c)
	if len(rest) != 1 || rest[0].ID != pks[1].ID {
		t.Fatalf("rest = %+v", rest)
	}
	if w := c.do("DELETE", "/api/admin/passkeys/"+rest[0].ID, nil); w.Code != 409 {
		t.Fatalf("delete last again = %d", w.Code)
	}
	if l.audits(t, "passkey.registered") != 1 || l.audits(t, "passkey.removed") != 1 {
		t.Fatal("passkey changes not audited")
	}
}

func TestRevokeSessionEndsIt(t *testing.T) {
	t.Parallel()
	l, c1 := adminClient(t, "192.168.1.20")
	c2 := newClient(t, l.env, "192.168.1.21")
	l.fullLogin(c2)
	type item struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
		IP      string `json:"ip"`
	}
	var res struct {
		Sessions []item `json:"sessions"`
	}
	body(t, c1.must("GET", "/api/admin/sessions", nil, 200).Body, &res)
	var other, own string
	for _, s := range res.Sessions {
		switch {
		case s.Current:
			own = s.ID
		case s.IP == "192.168.1.21":
			other = s.ID
		}
	}
	if own != c1.cookies[SessionCookie] || other != c2.cookies[SessionCookie] {
		t.Fatalf("sessions = %+v", res.Sessions)
	}
	c1.must("DELETE", "/api/admin/sessions/"+other, nil, 200)
	if w := c2.do("GET", "/api/me", nil); w.Code != 401 {
		t.Fatalf("revoked session /me = %d", w.Code)
	}
	// Deleting one's own session logs out.
	c1.must("DELETE", "/api/admin/sessions/"+own, nil, 200)
	if _, ok := c1.cookies[SessionCookie]; ok {
		t.Fatal("cookie kept")
	}
	if w := c1.do("GET", "/api/me", nil); w.Code != 401 {
		t.Fatalf("own revoked /me = %d", w.Code)
	}
	if l.audits(t, "session.revoked") != 2 {
		t.Fatal("revocations not audited")
	}
}

func TestRegenerateRecoveryInvalidatesOld(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	var res struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	body(t, c.must("POST", "/api/admin/recovery/regenerate", nil, 200).Body, &res)
	if len(res.RecoveryCodes) != 10 {
		t.Fatalf("codes = %v", res.RecoveryCodes)
	}
	var n int
	if err := l.deps.Pool.QueryRow(context.Background(), `select count(*) from recovery_codes where used_at is null`).Scan(&n); err != nil || n != 10 {
		t.Fatalf("stored codes = %d %v", n, err)
	}
	try := func(code string) int {
		o := newClient(t, l.env, "192.168.1.30")
		if r := password(o, "thijs", "correct horse"); r.Code != 200 {
			t.Fatalf("password = %d", r.Code)
		}
		return o.do("POST", "/api/auth/recovery", map[string]string{"code": code}).Code
	}
	if code := try(l.codes[0]); code == 200 {
		t.Fatal("old recovery code still works")
	}
	if code := try(res.RecoveryCodes[0]); code != 200 {
		t.Fatalf("new recovery code = %d", code)
	}
}

func TestEmergencyPut(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	state := func() (bool, string) {
		var en bool
		var h string
		if err := l.deps.Pool.QueryRow(context.Background(), `select enabled, coalesce(password_hash,'') from emergency`).Scan(&en, &h); err != nil {
			t.Fatal(err)
		}
		return en, h
	}
	_, h0 := state()
	if w := c.do("PUT", "/api/admin/emergency", map[string]any{"enabled": true, "password": "kort"}); w.Code != 400 {
		t.Fatalf("short password = %d", w.Code)
	}
	c.must("PUT", "/api/admin/emergency", map[string]any{"enabled": false}, 200)
	if en, h := state(); en || h != h0 {
		t.Fatalf("disable: %v, hash changed=%v", en, h != h0)
	}
	c.must("PUT", "/api/admin/emergency", map[string]any{"enabled": true, "password": "een-heel-nieuw-wachtwoord"}, 200)
	if en, h := state(); !en || h == h0 {
		t.Fatalf("enable: %v", en)
	}
	// Enabling without any hash and password → 400.
	if _, err := l.deps.Pool.Exec(context.Background(), `update emergency set enabled = false, password_hash = null`); err != nil {
		t.Fatal(err)
	}
	if w := c.do("PUT", "/api/admin/emergency", map[string]any{"enabled": true}); w.Code != 400 {
		t.Fatalf("enable without password = %d", w.Code)
	}
}

func TestAuditListAndExport(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	for i := 0; i < 5; i++ {
		l.deps.Audit.Log(context.Background(), "owner", "test.event", netip.MustParseAddr("192.168.1.20"), map[string]any{"i": i})
	}
	type entry struct {
		ID     int64          `json:"id"`
		Action string         `json:"action"`
		Detail map[string]any `json:"detail"`
	}
	var page struct {
		Entries    []entry `json:"entries"`
		NextBefore int64   `json:"next_before"`
	}
	body(t, c.must("GET", "/api/admin/audit?limit=3", nil, 200).Body, &page)
	if len(page.Entries) != 3 || page.Entries[0].ID <= page.Entries[1].ID || page.Entries[0].Detail["i"] != float64(4) || page.NextBefore != page.Entries[2].ID {
		t.Fatalf("page = %+v", page)
	}
	var page2 struct {
		Entries []entry `json:"entries"`
	}
	body(t, c.must("GET", "/api/admin/audit?limit=3&before="+strconv.FormatInt(page.NextBefore, 10), nil, 200).Body, &page2)
	if len(page2.Entries) != 3 || page2.Entries[0].ID >= page.NextBefore {
		t.Fatalf("page2 = %+v", page2)
	}
	for _, q := range []string{"limit=0", "limit=201", "limit=x", "before=-1"} {
		if w := c.do("GET", "/api/admin/audit?"+q, nil); w.Code != 400 {
			t.Fatalf("%s = %d", q, w.Code)
		}
	}
	w := c.must("GET", "/api/admin/audit/export", nil, 200)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/x-ndjson") {
		t.Fatalf("content-type = %q", w.Header().Get("Content-Type"))
	}
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	var total int
	if err := l.deps.Pool.QueryRow(context.Background(), `select count(*) from audit_log`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if len(lines) < total-1 { // the export itself may be audited after streaming
		t.Fatalf("export lines = %d, rows = %d", len(lines), total)
	}
	var e entry
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil || e.Action == "" {
		t.Fatalf("line 0 = %q %v", lines[0], err)
	}
}
