package httpapi

import (
	"testing"
	"time"

	"github.com/civr/keystone/internal/settings"
	"github.com/civr/keystone/internal/totp"
)

// An authenticator, once confirmed, is required after the password, works
// only with the current code, once; an admin can remove a lost one.
func TestTOTP(t *testing.T) {
	t.Parallel()
	l, owner := adminClient(t, "192.168.1.20")
	l.settings(t, func(s *settings.Settings) {
		s.Login.PasskeyRequired, s.BruteForce.LoginPerAccountPerMinute = false, 100
	})
	c, id := signedIn(t, l, owner, "tom", "192.168.1.70")

	var begin struct{ Secret, URI string }
	body(t, c.must("POST", "/api/account/totp", nil, 200).Body, &begin)
	code := func(offset int64) string {
		v, err := totp.Code(begin.Secret, totp.Step(time.Now())+offset)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	c.must("POST", "/api/account/totp/confirm", map[string]string{"code": "000000"}, 400)
	c.must("POST", "/api/account/totp/confirm", map[string]string{"code": code(-1)}, 200)

	login := func(ip string) *client {
		n := newClient(t, l.env, ip)
		if r := password(n, "tom", "long enough secret"); r.Code != 200 || r.Body["next"] != "totp" {
			t.Fatalf("password = %d %v", r.Code, r.Body)
		}
		return n
	}
	n := login("192.168.1.71")
	n.must("POST", "/api/auth/passkey/begin", nil, 401) // the code comes first
	n.must("POST", "/api/auth/totp", map[string]string{"code": "123456"}, 401)
	n.must("POST", "/api/auth/totp", map[string]string{"code": code(0)}, 200)
	n.must("GET", "/api/me", nil, 200)

	again := login("192.168.1.72")
	again.must("POST", "/api/auth/totp", map[string]string{"code": code(0)}, 401) // used already

	owner.must("DELETE", "/api/admin/users/"+id+"/totp", nil, 200)
	if r := password(newClient(t, l.env, "192.168.1.73"), "tom", "long enough secret"); r.Code != 200 || r.Body["next"] != "done" {
		t.Fatalf("after reset = %d %v", r.Code, r.Body)
	}
}
