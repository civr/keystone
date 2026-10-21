package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/passkey/passkeytest"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

const emergencyPassword = "nood-wachtwoord-1234567"

// loginEnv is a Keystone whose setup is complete: owner thijs with one passkey
// (auth), recovery codes (codes) and an emergency password.
type loginEnv struct {
	*setupEnv
	auth  *passkeytest.TestAuthenticator
	codes []string
}

func newLoginEnv(t *testing.T) *loginEnv {
	t.Helper()
	s := newSetupEnv(t)
	s.deps.Secrets.SessionKey = bytes.Repeat([]byte{9}, 32)
	s.h = New(s.deps)
	c := newClient(t, s.env, "192.168.1.10")
	s.unlock(c)
	s.domain(c)
	s.ad(c)
	s.owner(c)
	a := s.passkey(c)
	c.must("POST", "/api/setup/emergency", map[string]string{"password": emergencyPassword}, 200)
	var res struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	w := c.must("POST", "/api/setup/finish", nil, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return &loginEnv{setupEnv: s, auth: a, codes: res.RecoveryCodes}
}

func (l *loginEnv) settings(t *testing.T, mut func(*settings.Settings)) {
	t.Helper()
	l.put(t, mut)
}

func (l *loginEnv) cfg(t *testing.T) settings.Settings {
	t.Helper()
	cfg, err := l.deps.Settings.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func password(c *client, user, pw string) *jsonResp {
	c.t.Helper()
	return decode(c.t, c.do("POST", "/api/auth/password", map[string]string{"username": user, "password": pw}))
}

type jsonResp struct {
	Code int
	Body map[string]any
}

func decode(t *testing.T, w interface {
	Result() *http.Response
}) *jsonResp {
	t.Helper()
	res := w.Result()
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return &jsonResp{Code: res.StatusCode, Body: m}
}

func (r *jsonResp) err() string { s, _ := r.Body["error"].(string); return s }

// passkeyLogin runs /auth/passkey/begin + finish with authenticator a.
func passkeyLogin(c *client, a *passkeytest.TestAuthenticator) {
	c.t.Helper()
	opts := c.must("POST", "/api/auth/passkey/begin", nil, 200).Body.Bytes()
	assertion, err := a.Assert(opts)
	if err != nil {
		c.t.Fatal(err)
	}
	c.must("POST", "/api/auth/passkey/finish", assertion, 200)
}

// fullLogin signs in with password + passkey.
func (l *loginEnv) fullLogin(c *client) {
	c.t.Helper()
	if r := password(c, "thijs", "correct horse"); r.Code != 200 || r.Body["next"] != "passkey" {
		c.t.Fatalf("password = %d %v", r.Code, r.Body)
	}
	passkeyLogin(c, l.auth)
}

func (l *loginEnv) session(t *testing.T, c *client) session.Session {
	t.Helper()
	sid, ok := c.cookies[SessionCookie]
	if !ok {
		t.Fatal("no session cookie")
	}
	sess, err := l.deps.Sessions.Get(context.Background(), sid, l.cfg(t), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func (l *loginEnv) audits(t *testing.T, action string) int {
	t.Helper()
	var n int
	if err := l.deps.Pool.QueryRow(context.Background(), `select count(*) from audit_log where action = $1`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func me(c *client) map[string]any {
	c.t.Helper()
	return decode(c.t, c.must("GET", "/api/me", nil, 200)).Body
}

func TestPasswordThenPasskeyCreatesSession(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	c := newClient(t, l.env, "192.168.1.11")

	// Passkey step without a password step and without a trusted device.
	if w := c.do("POST", "/api/auth/passkey/begin", nil); w.Code != http.StatusUnauthorized || errCode(t, w) != "password_required" {
		t.Fatalf("begin without pending = %d %s", w.Code, w.Body.String())
	}

	before := time.Now().Add(-time.Second)
	r := password(c, "thijs", "correct horse")
	if r.Code != 200 || r.Body["next"] != "passkey" {
		t.Fatalf("password = %d %v", r.Code, r.Body)
	}
	pending := c.cookies[PendingCookie]
	if !strings.HasPrefix(pending, "login.") {
		t.Fatalf("pending cookie = %q", pending)
	}
	if _, ok := c.cookies[SessionCookie]; ok {
		t.Fatal("session before passkey step")
	}
	ttl := l.deps.Valkey.TTL(context.Background(), "login:"+strings.TrimPrefix(pending, "login.")).Val()
	if ttl <= 4*time.Minute || ttl > 5*time.Minute {
		t.Fatalf("pending TTL = %v", ttl)
	}
	if w := c.do("GET", "/api/me", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("/me with only pending = %d", w.Code)
	}

	passkeyLogin(c, l.auth)
	if _, ok := c.cookies[PendingCookie]; ok {
		t.Fatal("pending cookie not cleared")
	}
	if n := l.deps.Valkey.Exists(context.Background(), "login:"+strings.TrimPrefix(pending, "login.")).Val(); n != 0 {
		t.Fatal("pending state not deleted")
	}
	sess := l.session(t, c)
	if sess.LastFullAuthAt.Before(before) || sess.LastPasskeyAt.Before(before) || sess.IP != "192.168.1.11" || sess.Emergency || sess.MustRegisterPasskey {
		t.Fatalf("session = %+v", sess)
	}
	devID, devAt, ok := session.VerifyDeviceAt(l.deps.Secrets.SessionKey, c.cookies[DeviceCookie], l.cfg(t), time.Now())
	if !ok || devID == "" || devAt.Before(before.Truncate(time.Second)) {
		t.Fatalf("device cookie = %q %v %v", devID, devAt, ok)
	}
	m := me(c)
	if m["username"] != "thijs" || m["display_name"] != "Thijs Willems" || m["given_name"] != "Thijs" ||
		m["email"] != "thijs@home.lan" || m["emergency"] != false || m["must_register_passkey"] != false || m["needs_stepup"] != false {
		t.Fatalf("/me = %v", m)
	}
	if l.audits(t, "login.success") != 1 {
		t.Fatal("login.success not audited")
	}

	// Replaying the old pending cookie (without the device cookie) gives nothing.
	device := c.cookies[DeviceCookie]
	delete(c.cookies, DeviceCookie)
	c.cookies[PendingCookie] = pending
	if w := c.do("POST", "/api/auth/passkey/begin", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("reused pending = %d %s", w.Code, w.Body.String())
	}
	delete(c.cookies, PendingCookie)
	c.cookies[DeviceCookie] = device

	// A second full login on the same browser keeps the device id.
	c.must("POST", "/api/auth/logout", nil, 200)
	if _, ok := c.cookies[SessionCookie]; ok {
		t.Fatal("logout kept session cookie")
	}
	l.fullLogin(c)
	id2, _, ok := session.VerifyDeviceAt(l.deps.Secrets.SessionKey, c.cookies[DeviceCookie], l.cfg(t), time.Now())
	if !ok || id2 != devID {
		t.Fatalf("device id changed: %q → %q", devID, id2)
	}
	// A different browser gets a different device id.
	other := newClient(t, l.env, "192.168.1.12")
	l.fullLogin(other)
	id3, _, _ := session.VerifyDeviceAt(l.deps.Secrets.SessionKey, other.cookies[DeviceCookie], l.cfg(t), time.Now())
	if id3 == "" || id3 == devID {
		t.Fatalf("second browser device id = %q", id3)
	}
}

func TestPasswordOnlyWhenPasskeyNotRequired(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	l.settings(t, func(s *settings.Settings) { s.Login.PasskeyRequired = false })
	c := newClient(t, l.env, "192.168.1.13")
	r := password(c, "thijs", "correct horse")
	if r.Code != 200 || r.Body["next"] != "done" {
		t.Fatalf("password = %d %v", r.Code, r.Body)
	}
	if _, ok := c.cookies[PendingCookie]; ok {
		t.Fatal("pending cookie set although session was created")
	}
	sess := l.session(t, c)
	if time.Since(sess.LastFullAuthAt) > time.Minute || !sess.LastPasskeyAt.IsZero() {
		t.Fatalf("session = %+v", sess)
	}
	if me(c)["needs_stepup"] != true {
		t.Fatal("password-only session should need step-up")
	}
}

func TestNonOwnerRejected403(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	c := newClient(t, l.env, "192.168.1.14")
	r := password(c, "bob", "bobpw")
	if r.Code != http.StatusForbidden || r.err() != "not_allowed" || r.Body["message"] == "" {
		t.Fatalf("bob = %d %v", r.Code, r.Body)
	}
	if _, ok := c.cookies[PendingCookie]; ok {
		t.Fatal("non-owner got a pending cookie")
	}
	if l.audits(t, "login.not_allowed") != 1 {
		t.Fatal("not_allowed not audited")
	}
	// A wrong password for a non-owner looks exactly like one for the owner.
	for _, u := range [][2]string{{"bob", "nope"}, {"thijs", "nope"}, {"nobody", "x"}} {
		r := password(c, u[0], u[1])
		if r.Code != http.StatusUnauthorized || r.err() != "invalid_credentials" {
			t.Fatalf("%s wrong password = %d %v", u[0], r.Code, r.Body)
		}
	}
	if l.audits(t, "login.failure") != 3 {
		t.Fatalf("login.failure audits = %d", l.audits(t, "login.failure"))
	}
}

func TestUsernameVariantMatchesOwner(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	for i, u := range []string{`HOME\THIJS`, "thijs@home.lan", " Thijs "} {
		c := newClient(t, l.env, "192.168.1.15")
		r := password(c, u, "correct horse")
		if r.Code != 200 || r.Body["next"] != "passkey" {
			t.Fatalf("%d %q = %d %v", i, u, r.Code, r.Body)
		}
	}
}

func TestADDownReturns503Fast(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	if _, err := l.deps.Pool.Exec(context.Background(),
		`update ad_config set data = jsonb_set(data, '{timeout_seconds}', '1')`); err != nil {
		t.Fatal(err)
	}
	l.dir.Delay = 10 * time.Second
	c := newClient(t, l.env, "192.168.1.16")
	start := time.Now()
	r := password(c, "thijs", "correct horse")
	if el := time.Since(start); el > 1500*time.Millisecond {
		t.Fatalf("took %v", el)
	}
	if r.Code != http.StatusServiceUnavailable || r.err() != "ad_unavailable" || r.Body["emergency"] != true || r.Body["message"] != "AD is niet bereikbaar." {
		t.Fatalf("AD slow = %d %v", r.Code, r.Body)
	}
	if _, err := l.deps.Pool.Exec(context.Background(), `update emergency set enabled = false`); err != nil {
		t.Fatal(err)
	}
	if r := password(c, "thijs", "correct horse"); r.Code != http.StatusServiceUnavailable || r.Body["emergency"] != false {
		t.Fatalf("AD slow, emergency off = %d %v", r.Code, r.Body)
	}
}

func TestLockoutAfterFailures(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	l.settings(t, func(s *settings.Settings) {
		s.BruteForce.LockoutThreshold = 3
		s.BruteForce.LoginPerAccountPerMinute = 100
		s.BruteForce.LoginPerIPPerMinute = 100
	})
	c := newClient(t, l.env, "192.168.1.17")
	// A success resets the failure counter.
	for i := 0; i < 2; i++ {
		if r := password(c, "thijs", "wrong"); r.Code != http.StatusUnauthorized {
			t.Fatalf("wrong %d = %d %v", i, r.Code, r.Body)
		}
	}
	if r := password(c, "thijs", "correct horse"); r.Code != 200 {
		t.Fatalf("correct = %d %v", r.Code, r.Body)
	}
	for i := 0; i < 2; i++ {
		if r := password(c, `HOME\thijs`, "wrong"); r.Code != http.StatusUnauthorized {
			t.Fatalf("wrong after reset %d = %d %v", i, r.Code, r.Body)
		}
	}
	// Third consecutive failure locks (counted per normalized username).
	r := password(c, "THIJS", "wrong")
	if r.Code != http.StatusTooManyRequests || r.err() != "locked" {
		t.Fatalf("third failure = %d %v", r.Code, r.Body)
	}
	r = password(c, "thijs", "correct horse")
	if retry, _ := r.Body["retry_after"].(float64); r.Code != http.StatusTooManyRequests || r.err() != "locked" || retry < 1 || retry > 15*60 {
		t.Fatalf("correct while locked = %d %v", r.Code, r.Body)
	}
	if l.audits(t, "login.locked") < 1 {
		t.Fatal("login.locked not audited")
	}

	// Per-IP limit.
	l.settings(t, func(s *settings.Settings) { s.BruteForce.LoginPerIPPerMinute = 2 })
	ipc := newClient(t, l.env, "192.168.1.18")
	password(ipc, "bob", "x")
	password(ipc, "bob", "x")
	if r := password(ipc, "bob", "bobpw"); r.Code != http.StatusTooManyRequests || r.err() != "rate_limited" {
		t.Fatalf("per-IP limit = %d %v", r.Code, r.Body)
	}
}

func TestTrustedDevicePasskeyOnly(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	c := newClient(t, l.env, "192.168.1.19")
	l.fullLogin(c)
	_, fullAt, _ := session.VerifyDeviceAt(l.deps.Secrets.SessionKey, c.cookies[DeviceCookie], l.cfg(t), time.Now())
	c.must("POST", "/api/auth/logout", nil, 200)

	time.Sleep(1100 * time.Millisecond) // so a re-signed device cookie would differ
	device := c.cookies[DeviceCookie]
	passkeyLogin(c, l.auth)
	sess := l.session(t, c)
	if !sess.LastFullAuthAt.Equal(fullAt) || time.Since(sess.LastPasskeyAt) > time.Minute {
		t.Fatalf("passkey-only session = %+v, want LastFullAuthAt %v", sess, fullAt)
	}
	if c.cookies[DeviceCookie] != device {
		t.Fatal("passkey-only login re-signed the device cookie")
	}

	// Turned off: the trusted device no longer skips the password.
	c.must("POST", "/api/auth/logout", nil, 200)
	l.settings(t, func(s *settings.Settings) { s.Login.PasskeyOnlyOnTrustedDevice = false })
	if w := c.do("POST", "/api/auth/passkey/begin", nil); w.Code != http.StatusUnauthorized || errCode(t, w) != "password_required" {
		t.Fatalf("begin with setting off = %d %s", w.Code, w.Body.String())
	}
}

func TestExpiredDeviceNeedsPassword(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	key := l.deps.Secrets.SessionKey
	for name, cookie := range map[string]string{
		"expired": session.SignDevice(key, "dev-old", time.Now().Add(-91*24*time.Hour)),
		"forged":  session.SignDevice(bytes.Repeat([]byte{1}, 32), "dev-x", time.Now()),
		"garbage": "abc",
	} {
		c := newClient(t, l.env, "192.168.1.21")
		c.cookies[DeviceCookie] = cookie
		if w := c.do("POST", "/api/auth/passkey/begin", nil); w.Code != http.StatusUnauthorized || errCode(t, w) != "password_required" {
			t.Fatalf("%s device = %d %s", name, w.Code, w.Body.String())
		}
	}
	// Shortening trusted_device_days applies to existing cookies.
	c := newClient(t, l.env, "192.168.1.22")
	c.cookies[DeviceCookie] = session.SignDevice(key, "dev-1", time.Now().Add(-10*24*time.Hour))
	c.must("POST", "/api/auth/passkey/begin", nil, 200)
	l.settings(t, func(s *settings.Settings) { s.Session.TrustedDeviceDays = 7 })
	if w := c.do("POST", "/api/auth/passkey/begin", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("device older than shortened setting = %d", w.Code)
	}
}

func TestRecoveryCodeSingleUse(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	c := newClient(t, l.env, "192.168.1.23")
	if w := c.do("POST", "/api/auth/recovery", map[string]string{"code": l.codes[0]}); w.Code != http.StatusUnauthorized || errCode(t, w) != "password_required" {
		t.Fatalf("recovery without pending = %d %s", w.Code, w.Body.String())
	}
	password(c, "thijs", "correct horse")
	if w := c.do("POST", "/api/auth/recovery", map[string]string{"code": "AAAA-BBBB-CCCC-DDDD"}); w.Code != http.StatusUnauthorized || errCode(t, w) != "invalid_code" {
		t.Fatalf("wrong code = %d %s", w.Code, w.Body.String())
	}
	c.must("POST", "/api/auth/recovery", map[string]string{"code": " " + strings.ToLower(l.codes[0]) + " "}, 200)
	if _, ok := c.cookies[PendingCookie]; ok {
		t.Fatal("pending not cleared")
	}
	sess := l.session(t, c)
	if !sess.MustRegisterPasskey {
		t.Fatalf("session = %+v", sess)
	}
	if me(c)["must_register_passkey"] != true {
		t.Fatal("/me must_register_passkey not true")
	}
	if l.audits(t, "recovery.used") != 1 {
		t.Fatal("recovery.used not audited")
	}
	var unused int
	_ = l.deps.Pool.QueryRow(context.Background(), `select count(*) from recovery_codes where used_at is null`).Scan(&unused)
	if unused != 9 {
		t.Fatalf("unused codes = %d", unused)
	}

	again := newClient(t, l.env, "192.168.1.24")
	password(again, "thijs", "correct horse")
	if w := again.do("POST", "/api/auth/recovery", map[string]string{"code": l.codes[0]}); w.Code != http.StatusUnauthorized || errCode(t, w) != "invalid_code" {
		t.Fatalf("reused code = %d %s", w.Code, w.Body.String())
	}
	again.must("POST", "/api/auth/recovery", map[string]string{"code": l.codes[1]}, 200)
}

func TestMustRegisterPasskeyGate(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	c := newClient(t, l.env, "192.168.1.25")
	password(c, "thijs", "correct horse")
	c.must("POST", "/api/auth/recovery", map[string]string{"code": l.codes[0]}, 200)

	for _, p := range [][2]string{{"POST", "/api/auth/stepup/begin"}, {"GET", "/api/setup/status"}, {"GET", "/api/admin/settings"}, {"GET", "/api/apps"}} {
		if w := c.do(p[0], p[1], nil); w.Code != http.StatusForbidden || errCode(t, w) != "must_register_passkey" {
			t.Fatalf("%s %s = %d %s", p[0], p[1], w.Code, w.Body.String())
		}
	}
	if w := c.do("GET", "/", nil); w.Code != 200 {
		t.Fatalf("shell = %d", w.Code)
	}

	opts := c.must("POST", "/api/auth/passkey/begin", nil, 200).Body.Bytes()
	if !bytes.Contains(opts, []byte(`"rp"`)) {
		t.Fatalf("expected creation options, got %s", opts)
	}
	a := passkeytest.NewTestAuthenticator(setupOrigin)
	cred, err := a.Register(opts)
	if err != nil {
		t.Fatal(err)
	}
	c.must("POST", "/api/auth/passkey/finish?name=iPhone", cred, 200)
	sess := l.session(t, c)
	if sess.MustRegisterPasskey || time.Since(sess.LastPasskeyAt) > time.Minute {
		t.Fatalf("session after registration = %+v", sess)
	}
	if m := me(c); m["must_register_passkey"] != false || m["needs_stepup"] != false {
		t.Fatalf("/me = %v", m)
	}
	c.must("POST", "/api/auth/stepup/begin", nil, 200)
	var n int
	_ = l.deps.Pool.QueryRow(context.Background(), `select count(*) from passkeys where name = 'iPhone'`).Scan(&n)
	if n != 1 {
		t.Fatal("new passkey not stored")
	}
	// The new passkey works for a normal login.
	other := newClient(t, l.env, "192.168.1.26")
	password(other, "thijs", "correct horse")
	passkeyLogin(other, a)

	// Logout is allowed while gated.
	g := newClient(t, l.env, "192.168.1.27")
	password(g, "thijs", "correct horse")
	g.must("POST", "/api/auth/recovery", map[string]string{"code": l.codes[1]}, 200)
	g.must("POST", "/api/auth/logout", nil, 200)
}

func TestEmergencyOnlyWhenADDown(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	c := newClient(t, l.env, "192.168.1.28")
	if w := c.do("POST", "/api/auth/emergency", map[string]string{"password": emergencyPassword}); w.Code != http.StatusForbidden || errCode(t, w) != "ad_available" {
		t.Fatalf("emergency with AD up = %d %s", w.Code, w.Body.String())
	}
	if _, ok := c.cookies[PendingCookie]; ok {
		t.Fatal("pending set with AD up")
	}
	l.dir.Down = true
	if r := password(c, "thijs", "correct horse"); r.Code != http.StatusServiceUnavailable || r.Body["emergency"] != true {
		t.Fatalf("password with AD down = %d %v", r.Code, r.Body)
	}
	if w := c.do("POST", "/api/auth/emergency", map[string]string{"password": "wrong-wrong-wrong-wrong"}); w.Code != http.StatusUnauthorized || errCode(t, w) != "invalid_credentials" {
		t.Fatalf("wrong emergency password = %d %s", w.Code, w.Body.String())
	}
	w := c.must("POST", "/api/auth/emergency", map[string]string{"password": emergencyPassword}, 200)
	if r := decode(t, w); r.Body["next"] != "passkey" {
		t.Fatalf("emergency = %v", r.Body)
	}
	if _, ok := c.cookies[SessionCookie]; ok {
		t.Fatal("emergency created a session without passkey")
	}
	passkeyLogin(c, l.auth)
	if !l.session(t, c).Emergency || me(c)["emergency"] != true {
		t.Fatal("session not flagged emergency")
	}
	if l.audits(t, "emergency.used") != 1 {
		t.Fatal("emergency.used not audited")
	}

	if _, err := l.deps.Pool.Exec(context.Background(), `update emergency set enabled = false`); err != nil {
		t.Fatal(err)
	}
	d := newClient(t, l.env, "192.168.1.29")
	if w := d.do("POST", "/api/auth/emergency", map[string]string{"password": emergencyPassword}); w.Code != http.StatusForbidden || errCode(t, w) != "emergency_disabled" {
		t.Fatalf("disabled emergency = %d %s", w.Code, w.Body.String())
	}
}

func TestStepUpRequired(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	srv := newServer(l.deps)
	r := chi.NewRouter()
	r.Use(srv.middlewares()...)
	r.With(requireStepUp).Post("/api/test/protected", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	c := newClient(t, l.env, "192.168.1.30")
	pc := &client{t: t, h: r, ip: c.ip, host: c.host, origin: c.origin, cookies: c.cookies}

	if w := pc.do("POST", "/api/test/protected", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d", w.Code)
	}
	l.fullLogin(c)
	pc.must("POST", "/api/test/protected", nil, 200)

	sess := l.session(t, c)
	sess.LastPasskeyAt = time.Now().Add(-6 * time.Minute)
	if err := l.deps.Sessions.Update(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if w := pc.do("POST", "/api/test/protected", nil); w.Code != http.StatusForbidden || errCode(t, w) != "stepup_required" {
		t.Fatalf("stale passkey = %d %s", w.Code, w.Body.String())
	}
	if me(c)["needs_stepup"] != true {
		t.Fatal("/me needs_stepup not true")
	}
	l.settings(t, func(s *settings.Settings) { s.Login.StepUpMinutes = 10 })
	pc.must("POST", "/api/test/protected", nil, 200)
	l.settings(t, func(s *settings.Settings) { s.Login.StepUpMinutes = 5 })

	if w := c.do("POST", "/api/auth/stepup/finish", []byte(`{}`)); w.Code != http.StatusBadRequest || errCode(t, w) != "no_ceremony" {
		t.Fatalf("finish without begin = %d %s", w.Code, w.Body.String())
	}
	opts := c.must("POST", "/api/auth/stepup/begin", nil, 200).Body.Bytes()
	assertion, err := l.auth.Assert(opts)
	if err != nil {
		t.Fatal(err)
	}
	c.must("POST", "/api/auth/stepup/finish", assertion, 200)
	pc.must("POST", "/api/test/protected", nil, 200)
	if me(c)["needs_stepup"] != false {
		t.Fatal("needs_stepup after step-up")
	}
	if l.audits(t, "stepup") != 1 {
		t.Fatal("stepup not audited")
	}
	after := l.session(t, c)
	if !after.LastFullAuthAt.Equal(sess.LastFullAuthAt) {
		t.Fatal("step-up changed LastFullAuthAt")
	}
}

func sessionCookie(w interface{ Result() *http.Response }) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	return nil
}

func TestSlidingSessionRefreshesCookie(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	c := newClient(t, l.env, "192.168.1.40")
	l.fullLogin(c)
	sid := c.cookies[SessionCookie]
	ctx := context.Background()
	backdate := func() {
		if err := l.deps.Valkey.HSet(ctx, "sess:"+sid, "seen", time.Now().Add(-2*time.Hour).UnixNano()).Err(); err != nil {
			t.Fatal(err)
		}
	}

	backdate()
	ck := sessionCookie(c.must("GET", "/api/me", nil, 200))
	if ck == nil || ck.Value != sid || ck.MaxAge != 720*3600 || !ck.HttpOnly {
		t.Fatalf("refreshed cookie = %+v", ck)
	}
	hour := time.Now().Truncate(time.Hour)
	w := c.must("GET", "/api/me", nil, 200)
	if ck := sessionCookie(w); ck != nil && time.Now().Truncate(time.Hour).Equal(hour) {
		t.Fatalf("cookie re-issued again within the same hour: %+v", ck)
	}

	l.settings(t, func(s *settings.Settings) { s.Session.Sliding = false })
	backdate()
	if ck := sessionCookie(c.must("GET", "/api/me", nil, 200)); ck != nil {
		t.Fatalf("non-sliding session re-issued cookie: %+v", ck)
	}
}

func TestLoginBlockedBeforeSetupDone(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.41")
	s.unlock(c)
	s.domain(c)
	s.ad(c)
	s.owner(c) // owner exists, no passkey, setup not finished

	att := newClient(t, s.env, "192.168.1.42")
	for _, p := range []string{"/api/auth/password", "/api/auth/passkey/begin", "/api/auth/passkey/finish",
		"/api/auth/recovery", "/api/auth/emergency", "/api/auth/stepup/begin", "/api/auth/stepup/finish", "/api/auth/logout"} {
		w := att.do("POST", p, map[string]string{"username": "thijs", "password": "correct horse"})
		if w.Code != http.StatusConflict || errCode(t, w) != "setup_needed" {
			t.Fatalf("%s before setup = %d %s", p, w.Code, w.Body.String())
		}
	}
	if len(att.cookies) != 0 {
		t.Fatalf("cookies set before setup: %v", att.cookies)
	}
	s.passkey(c)
	c.must("POST", "/api/setup/finish", nil, 200)
	if r := password(att, "thijs", "correct horse"); r.Code != 200 {
		t.Fatalf("password after setup = %d %v", r.Code, r.Body)
	}
}

// errDir is a Directory that always fails with err.
type errDir struct{ err error }

func (d errDir) Authenticate(context.Context, string, string) (ad.User, error) {
	return ad.User{}, d.err
}
func (d errDir) Lookup(context.Context, string) (ad.User, error) { return ad.User{}, d.err }

func TestADErrorClassification(t *testing.T) {
	t.Parallel()
	l := newLoginEnv(t)
	with := func(err error) *client {
		l.deps.Dir = func() ad.Directory { return errDir{err} }
		l.h = New(l.deps)
		return newClient(t, l.env, "192.168.1.43")
	}

	// Anything but ErrUnavailable is an internal error, not "AD down".
	c := with(errors.New("ldap: unexpected result code 53"))
	if r := password(c, "thijs", "correct horse"); r.Code != http.StatusInternalServerError || r.err() != "internal" {
		t.Fatalf("password, other AD error = %d %v", r.Code, r.Body)
	}
	if w := c.do("POST", "/api/auth/emergency", map[string]string{"password": emergencyPassword}); w.Code != http.StatusInternalServerError || errCode(t, w) != "internal" {
		t.Fatalf("emergency, other AD error = %d %s", w.Code, w.Body.String())
	}

	// A wrapped ErrUnavailable is AD down for both endpoints.
	c = with(fmt.Errorf("dial: %w", ad.ErrUnavailable))
	if r := password(c, "thijs", "correct horse"); r.Code != http.StatusServiceUnavailable || r.err() != "ad_unavailable" || r.Body["emergency"] != true {
		t.Fatalf("password, AD unavailable = %d %v", r.Code, r.Body)
	}
	if r := decode(t, c.do("POST", "/api/auth/emergency", map[string]string{"password": emergencyPassword})); r.Code != 200 || r.Body["next"] != "passkey" {
		t.Fatalf("emergency, AD unavailable = %d %v", r.Code, r.Body)
	}

	// Enabled without a password hash is not an emergency option.
	if _, err := l.deps.Pool.Exec(context.Background(), `update emergency set enabled = true, password_hash = null`); err != nil {
		t.Fatal(err)
	}
	if r := password(c, "thijs", "correct horse"); r.Code != http.StatusServiceUnavailable || r.Body["emergency"] != false {
		t.Fatalf("emergency without hash = %d %v", r.Code, r.Body)
	}
}
