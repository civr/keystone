package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/passkey/passkeytest"
	"github.com/civr/keystone/internal/session"
)

const (
	setupHost   = "keystone.home.lan"
	setupOrigin = "https://keystone.home.lan"
)

// client is a tiny browser: fixed IP, Host and Origin, and a cookie jar.
type client struct {
	t       *testing.T
	h       http.Handler
	ip      string
	host    string // request Host; setupHost by default
	origin  string // Origin header; setupOrigin by default
	cookies map[string]string
	headers map[string]string // extra request headers
}

func newClient(t *testing.T, e *env, ip string) *client {
	return &client{t: t, h: e.h, ip: ip, host: setupHost, origin: setupOrigin, cookies: map[string]string{}}
}

func (c *client) do(method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Host = c.host
	r.RemoteAddr = c.ip + ":40000"
	r.Header.Set("Origin", c.origin)
	r.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		r.Header.Set(k, v)
	}
	for k, v := range c.cookies {
		r.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	for _, ck := range w.Result().Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck.Value
		}
	}
	return w
}

func (c *client) must(method, path string, body any, want int) *httptest.ResponseRecorder {
	c.t.Helper()
	w := c.do(method, path, body)
	if w.Code != want {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}

type setupEnv struct {
	*env
	code string
	dir  *ad.Fake
}

func newSetupEnv(t *testing.T) *setupEnv {
	t.Helper()
	e := newEnv(t, true)
	code := crypto.RecoveryCode()
	hash, err := crypto.HashSecret(code)
	if err != nil {
		t.Fatal(err)
	}
	dir := &ad.Fake{Users: map[string]ad.FakeUser{
		"thijs": {Password: "correct horse", DisplayName: "Thijs Willems", GivenName: "Thijs", Email: "thijs@home.lan"},
		"bob":   {Password: "bobpw"},
	}}
	e.deps.SetupCodeHash = hash
	e.deps.Secrets.DataKey = bytes.Repeat([]byte{7}, 32)
	e.deps.Secrets.VaultKey = bytes.Repeat([]byte{8}, 32)
	e.deps.Dir = func() ad.Directory { return dir }
	e.deps.TestAD = func(context.Context, ad.Config) (int, error) { return 42, nil }
	e.h = New(e.deps)
	return &setupEnv{env: e, code: code, dir: dir}
}

var adBody = map[string]any{
	"url": "ldaps://dc.home.lan", "bind_dn": "cn=svc,dc=home,dc=lan", "bind_password": "svc-secret", "base_dn": "dc=home,dc=lan",
}

func (s *setupEnv) unlock(c *client) {
	c.must("POST", "/api/setup/unlock", map[string]string{"code": s.code}, 200)
}

func (s *setupEnv) domain(c *client) {
	c.must("POST", "/api/setup/domain", map[string]string{"domain": "home.lan", "public_url": setupOrigin}, 200)
}

func (s *setupEnv) ad(c *client) { c.must("POST", "/api/setup/ad", adBody, 200) }

func (s *setupEnv) owner(c *client) {
	c.must("POST", "/api/setup/owner", map[string]string{"username": `HOME\Thijs`, "password": "correct horse"}, 200)
}

func (s *setupEnv) passkey(c *client) *passkeytest.TestAuthenticator {
	c.t.Helper()
	opts := c.must("POST", "/api/setup/passkey/begin", nil, 200).Body.Bytes()
	a := passkeytest.NewTestAuthenticator(setupOrigin)
	cred, err := a.Register(opts)
	if err != nil {
		c.t.Fatal(err)
	}
	c.must("POST", "/api/setup/passkey/finish?name=MacBook", cred, 200)
	return a
}

func status(t *testing.T, c *client) (bool, string) {
	t.Helper()
	var st struct {
		Needed bool   `json:"needed"`
		Step   string `json:"step"`
	}
	w := c.must("GET", "/api/setup/status", nil, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st.Needed, st.Step
}

func TestSetupRequiresUnlock(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.20")
	if needed, step := status(t, c); !needed || step != "unlock" {
		t.Fatalf("status = %v %q, want true unlock", needed, step)
	}
	for _, path := range []string{"/api/setup/domain", "/api/setup/ad/test", "/api/setup/ad", "/api/setup/owner",
		"/api/setup/passkey/begin", "/api/setup/passkey/finish", "/api/setup/emergency", "/api/setup/finish"} {
		w := c.do("POST", path, map[string]string{})
		if w.Code != http.StatusUnauthorized || errCode(t, w) != "setup_locked" {
			t.Fatalf("%s without unlock = %d %s", path, w.Code, w.Body.String())
		}
	}
	c.cookies[PendingCookie] = "setup.forged-token"
	if w := c.do("POST", "/api/setup/domain", map[string]string{}); w.Code != http.StatusUnauthorized {
		t.Fatalf("forged cookie = %d", w.Code)
	}
	delete(c.cookies, PendingCookie)

	w := c.must("POST", "/api/setup/unlock", map[string]string{"code": strings.ToLower(s.code)}, 200)
	ck := w.Result().Cookies()
	// The client's page is https (setupOrigin), so the cookie is Secure.
	if len(ck) != 1 || ck[0].Name != PendingCookie || !ck[0].HttpOnly || ck[0].SameSite != http.SameSiteLaxMode || !ck[0].Secure {
		t.Fatalf("pending cookie = %+v", ck)
	}
	if needed, step := status(t, c); !needed || step != "domain" {
		t.Fatalf("status after unlock = %v %q", needed, step)
	}
}

func TestWrongSetupCodeRateLimited(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.30")
	awayFromMinuteEdge(t)
	// Successful unlocks do not count towards the limit.
	for i := 0; i < 6; i++ {
		c.must("POST", "/api/setup/unlock", map[string]string{"code": s.code}, 200)
	}
	delete(c.cookies, PendingCookie)
	for i := 0; i < 5; i++ {
		w := c.do("POST", "/api/setup/unlock", map[string]string{"code": "AAAA-BBBB-CCCC-DDDD"})
		if w.Code != http.StatusUnauthorized || errCode(t, w) != "bad_code" {
			t.Fatalf("attempt %d = %d %s", i+1, w.Code, w.Body.String())
		}
	}
	w := c.do("POST", "/api/setup/unlock", map[string]string{"code": s.code})
	if w.Code != http.StatusTooManyRequests || errCode(t, w) != "rate_limited" {
		t.Fatalf("6th attempt = %d %s", w.Code, w.Body.String())
	}
	if _, ok := c.cookies[PendingCookie]; ok {
		t.Fatal("rate-limited unlock set a cookie")
	}
	other := newClient(t, s.env, "192.168.1.31")
	s.unlock(other)
}

func TestSetupDomainAndAD(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.40")
	s.unlock(c)
	for _, bad := range []map[string]string{
		{"domain": "home.lan", "public_url": "ftp://keystone.home.lan"},
		{"domain": "home.lan", "public_url": "https://keystone.other.lan"},
		{"domain": "home.lan", "public_url": "https://keystone.home.lan/sub"},
		{"domain": "", "public_url": setupOrigin},
	} {
		if w := c.do("POST", "/api/setup/domain", bad); w.Code != http.StatusBadRequest {
			t.Fatalf("domain %v = %d", bad, w.Code)
		}
	}
	c.must("POST", "/api/setup/domain", map[string]string{"domain": "Home.LAN", "public_url": "https://Keystone.home.lan/"}, 200)
	cfg, err := s.deps.Settings.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Domain != "home.lan" || cfg.PublicURL != setupOrigin {
		t.Fatalf("settings domain=%q public_url=%q", cfg.Domain, cfg.PublicURL)
	}

	w := c.must("POST", "/api/setup/ad/test", adBody, 200)
	var res struct {
		OK         bool   `json:"ok"`
		Error      string `json:"error"`
		UsersFound int    `json:"users_found"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || !res.OK || res.UsersFound != 42 {
		t.Fatalf("ad/test = %s", w.Body.String())
	}
	plain := map[string]any{"url": "ldap://dc.home.lan", "bind_dn": "x", "bind_password": "y", "base_dn": "z"}
	if w := c.do("POST", "/api/setup/ad", plain); w.Code != http.StatusBadRequest {
		t.Fatalf("plaintext ad config = %d", w.Code)
	}
	noPW := map[string]any{"url": "ldaps://dc.home.lan", "bind_dn": "x", "base_dn": "z"}
	if w := c.do("POST", "/api/setup/ad", noPW); w.Code != http.StatusBadRequest {
		t.Fatalf("ad config without bind_password = %d", w.Code)
	}
	s.ad(c)

	var raw []byte
	if err := s.deps.Pool.QueryRow(context.Background(), `select data from ad_config where id = 1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("svc-secret")) || bytes.Contains(raw, []byte(`"bind_password"`)) {
		t.Fatalf("plain bind password stored: %s", raw)
	}
	var stored ad.Config
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	pw, err := crypto.Open(s.deps.Secrets.DataKey, stored.BindPasswordSealed)
	if err != nil || string(pw) != "svc-secret" || stored.URL != "ldaps://dc.home.lan" || stored.BaseDN != "dc=home,dc=lan" {
		t.Fatalf("stored ad config %+v (pw %q, %v)", stored, pw, err)
	}
	if _, step := status(t, c); step != "owner" {
		t.Fatalf("step = %q, want owner", step)
	}
}

func TestOwnerMustAuthenticateAgainstAD(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.50")
	s.unlock(c)
	s.domain(c)
	s.ad(c)

	w := c.do("POST", "/api/setup/owner", map[string]string{"username": "thijs", "password": "wrong"})
	if w.Code != http.StatusUnauthorized || errCode(t, w) != "invalid_credentials" {
		t.Fatalf("wrong password = %d %s", w.Code, w.Body.String())
	}
	w = c.do("POST", "/api/setup/owner", map[string]string{"username": "nobody", "password": "x"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user = %d", w.Code)
	}
	s.dir.Down = true
	w = c.do("POST", "/api/setup/owner", map[string]string{"username": "thijs", "password": "correct horse"})
	if w.Code != http.StatusServiceUnavailable || errCode(t, w) != "ad_unavailable" {
		t.Fatalf("AD down = %d %s", w.Code, w.Body.String())
	}
	s.dir.Down = false
	var n int
	if err := s.deps.Pool.QueryRow(context.Background(), `select count(*) from owner`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("owner rows after failures = %d, %v", n, err)
	}

	s.owner(c)
	var username, display, given, email string
	var handle []byte
	if err := s.deps.Pool.QueryRow(context.Background(),
		`select username, display_name, given_name, email, user_handle from owner where id = 1`).
		Scan(&username, &display, &given, &email, &handle); err != nil {
		t.Fatal(err)
	}
	if username != "thijs" || display != "Thijs Willems" || given != "Thijs" || email != "thijs@home.lan" || len(handle) != 64 {
		t.Fatalf("owner = %q %q %q %q handle %d bytes", username, display, given, email, len(handle))
	}
}

func TestStepsEnforcedInOrder(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.60")
	s.unlock(c)
	conflict := func(path string, body any) {
		t.Helper()
		w := c.do("POST", path, body)
		if w.Code != http.StatusConflict || errCode(t, w) != "step_order" {
			t.Fatalf("%s = %d %s, want 409 step_order", path, w.Code, w.Body.String())
		}
	}
	conflict("/api/setup/ad", adBody)
	s.domain(c)
	conflict("/api/setup/owner", map[string]string{"username": "thijs", "password": "correct horse"})
	s.ad(c)
	conflict("/api/setup/passkey/begin", nil)
	conflict("/api/setup/emergency", map[string]string{"password": "a-very-long-emergency-pw"})
	conflict("/api/setup/finish", nil)
	s.owner(c)
	conflict("/api/setup/finish", nil)
	s.passkey(c)
	if _, step := status(t, c); step != "emergency" {
		t.Fatalf("step after passkey = %q", step)
	}
	// Once a passkey exists, the steps it depends on are frozen.
	conflict("/api/setup/domain", map[string]string{"domain": "home.lan", "public_url": setupOrigin})
	conflict("/api/setup/owner", map[string]string{"username": "bob", "password": "bobpw"})

	if w := c.do("POST", "/api/setup/emergency", map[string]string{"password": "too-short"}); w.Code != http.StatusBadRequest {
		t.Fatalf("short emergency password = %d", w.Code)
	}
	c.must("POST", "/api/setup/emergency", map[string]string{"password": "a-very-long-emergency-pw"}, 200)
	var enabled bool
	var hash string
	if err := s.deps.Pool.QueryRow(context.Background(), `select enabled, password_hash from emergency where id = 1`).Scan(&enabled, &hash); err != nil {
		t.Fatal(err)
	}
	if !enabled || !crypto.VerifySecret(hash, "a-very-long-emergency-pw") {
		t.Fatalf("emergency enabled=%v hash ok=%v", enabled, crypto.VerifySecret(hash, "a-very-long-emergency-pw"))
	}
	if _, step := status(t, c); step != "finish" {
		t.Fatalf("step after emergency = %q", step)
	}
}

func TestADResaveResetsOwner(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.65")
	s.unlock(c)
	s.domain(c)
	s.ad(c)
	s.owner(c)
	if _, step := status(t, c); step != "passkey" {
		t.Fatalf("step = %q, want passkey", step)
	}
	s.ad(c) // a different directory: the owner must be verified again
	if _, step := status(t, c); step != "owner" {
		t.Fatalf("step after AD resave = %q, want owner", step)
	}
	w := c.do("POST", "/api/setup/passkey/begin", nil)
	if w.Code != http.StatusConflict || errCode(t, w) != "step_order" {
		t.Fatalf("passkey/begin after AD resave = %d %s", w.Code, w.Body.String())
	}
	var n int
	if err := s.deps.Pool.QueryRow(context.Background(), `select count(*) from owner`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("owner rows after AD resave = %d, %v", n, err)
	}
	s.owner(c)
	s.passkey(c)
}

func TestSetupPasskeyFinishReplayRejected(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.66")
	s.unlock(c)
	s.domain(c)
	s.ad(c)
	s.owner(c)
	opts := c.must("POST", "/api/setup/passkey/begin", nil, 200).Body.Bytes()
	a := passkeytest.NewTestAuthenticator(setupOrigin)
	cred, err := a.Register(opts)
	if err != nil {
		t.Fatal(err)
	}
	c.must("POST", "/api/setup/passkey/finish", cred, 200)
	w := c.do("POST", "/api/setup/passkey/finish", cred)
	if w.Code != http.StatusBadRequest || errCode(t, w) != "no_ceremony" {
		t.Fatalf("replayed finish = %d %s", w.Code, w.Body.String())
	}
}

func completeSetup(t *testing.T, s *setupEnv, c *client) *httptest.ResponseRecorder {
	t.Helper()
	s.unlock(c)
	s.domain(c)
	s.ad(c)
	s.owner(c)
	s.passkey(c)
	return c.must("POST", "/api/setup/finish", nil, 200)
}

func TestFinishReturnsTenRecoveryCodesAndSession(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.70")
	w := completeSetup(t, s, c)
	var res struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.RecoveryCodes) != 10 {
		t.Fatalf("recovery codes = %v", res.RecoveryCodes)
	}
	re := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){3}$`)
	seen := map[string]bool{}
	for _, code := range res.RecoveryCodes {
		if !re.MatchString(code) || seen[code] {
			t.Fatalf("bad or duplicate code %q", code)
		}
		seen[code] = true
	}
	ctx := context.Background()
	rows, err := s.deps.Pool.Query(ctx, `select hash from recovery_codes where used_at is null`)
	if err != nil {
		t.Fatal(err)
	}
	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h)
	}
	rows.Close()
	if len(hashes) != 10 {
		t.Fatalf("stored %d recovery hashes", len(hashes))
	}
	for _, code := range res.RecoveryCodes {
		matches := 0
		for _, h := range hashes {
			if crypto.VerifySecret(h, code) {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("recovery code %s matches %d stored hashes, want 1", code, matches)
		}
	}
	for _, h := range hashes {
		if strings.Contains(h, res.RecoveryCodes[0]) {
			t.Fatal("recovery code stored in plain text")
		}
	}

	sid, ok := c.cookies[SessionCookie]
	if !ok {
		t.Fatal("no session cookie after finish")
	}
	if _, ok := c.cookies[PendingCookie]; ok {
		t.Fatal("pending cookie not cleared after finish")
	}
	cfg, _ := s.deps.Settings.Get(ctx)
	sess, err := s.deps.Sessions.Get(ctx, sid, cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(sess.LastFullAuthAt) > time.Minute || time.Since(sess.LastPasskeyAt) > time.Minute || sess.IP != "192.168.1.70" {
		t.Fatalf("session = %+v", sess)
	}
	var completed bool
	if err := s.deps.Pool.QueryRow(ctx, `select completed_at is not null from setup_state where id = 1`).Scan(&completed); err != nil || !completed {
		t.Fatalf("setup_state completed = %v, %v", completed, err)
	}
	var audited int
	if err := s.deps.Pool.QueryRow(ctx, `select count(*) from audit_log where action = 'setup.completed'`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("setup.completed audit rows = %d, %v", audited, err)
	}
}

func TestSetupGoneAfterFinish(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.80")
	completeSetup(t, s, c)
	if needed, step := status(t, c); needed || step != "done" {
		t.Fatalf("status after finish = %v %q", needed, step)
	}
	stranger := newClient(t, s.env, "192.168.1.81")
	for _, cl := range []*client{c, stranger} {
		for _, path := range []string{"/api/setup/unlock", "/api/setup/domain", "/api/setup/ad/test", "/api/setup/ad",
			"/api/setup/owner", "/api/setup/passkey/begin", "/api/setup/passkey/finish", "/api/setup/emergency", "/api/setup/finish"} {
			w := cl.do("POST", path, map[string]string{"code": s.code})
			if w.Code != http.StatusGone || errCode(t, w) != "setup_done" {
				t.Fatalf("%s after finish = %d %s", path, w.Code, w.Body.String())
			}
		}
	}
}

// A wrong public_url must not lock the operator out of the wizard: an Origin
// whose host equals the request Host is accepted (the unlock cookie is still
// required).
func TestSetupDomainRedoFromHostOrigin(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.90")
	c.host, c.origin = "192.168.1.5:8080", "http://192.168.1.5:8080"
	s.unlock(c)
	c.must("POST", "/api/setup/domain", map[string]string{"domain": "home.lan", "public_url": "https://typo.home.lan"}, 200)
	// The browser is still on the IP address: it can redo the domain step.
	s.domain(c)
	cfg, err := s.deps.Settings.Get(context.Background())
	if err != nil || cfg.PublicURL != setupOrigin {
		t.Fatalf("public_url = %q, %v", cfg.PublicURL, err)
	}
	// Another Origin than the Host is still rejected.
	c.origin = "http://evil.example"
	if w := c.do("POST", "/api/setup/domain", map[string]string{"domain": "home.lan", "public_url": setupOrigin}); w.Code != http.StatusForbidden {
		t.Fatalf("foreign origin = %d", w.Code)
	}
	// Re-unlocking over plain http after an https public_url was saved must
	// give a cookie the browser keeps (not Secure), or the wizard is stuck.
	c.origin = "http://192.168.1.5:8080"
	w := c.must("POST", "/api/setup/unlock", map[string]string{"code": s.code}, 200)
	if ck := w.Result().Cookies(); len(ck) != 1 || ck[0].Secure {
		t.Fatalf("unlock cookie over http = %+v", ck)
	}
	// From the public_url origin the cookie stays Secure.
	pub := newClient(t, s.env, "192.168.1.91")
	w = pub.must("POST", "/api/setup/unlock", map[string]string{"code": s.code}, 200)
	if ck := w.Result().Cookies(); len(ck) != 1 || !ck[0].Secure {
		t.Fatalf("unlock cookie from public_url = %+v", ck)
	}
}

func TestSetupHostOriginClosedAfterFinish(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.92")
	completeSetup(t, s, c)
	c.host, c.origin = "192.168.1.5:8080", "http://192.168.1.5:8080"
	if w := c.do("POST", "/api/setup/domain", map[string]string{"domain": "home.lan", "public_url": setupOrigin}); w.Code != http.StatusGone || errCode(t, w) != "setup_done" {
		t.Fatalf("setup from host origin after finish = %d %s", w.Code, w.Body.String())
	}
}

// The browser that completes setup becomes a trusted device.
func TestFinishMarksTrustedDevice(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	s.deps.Secrets.SessionKey = bytes.Repeat([]byte{9}, 32)
	s.h = New(s.deps)
	c := newClient(t, s.env, "192.168.1.93")
	completeSetup(t, s, c)
	cfg, err := s.deps.Settings.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, at, ok := session.VerifyDeviceAt(s.deps.Secrets.SessionKey, c.cookies[DeviceCookie], cfg, time.Now())
	if !ok || time.Since(at) > time.Minute {
		t.Fatalf("device cookie after finish: ok=%v at=%v", ok, at)
	}
}
