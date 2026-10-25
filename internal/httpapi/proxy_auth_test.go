package httpapi

import (
	"testing"

	"github.com/civr/keystone/internal/settings"
)

// The identity header counts only from a configured proxy address, as the
// last X-Forwarded-For hop (the one Caddy appends); a client sending the
// header itself, or naming a proxy earlier in the chain, gets nothing.
func TestProxyAuth(t *testing.T) {
	t.Parallel()
	l, owner := adminClient(t, "192.168.1.20")
	l.settings(t, func(s *settings.Settings) { s.Login.PasskeyRequired, s.BruteForce.LoginPerAccountPerMinute = false, 100 })
	signedIn(t, l, owner, "pia", "192.168.1.60")

	try := func(xff, user string, want int) {
		t.Helper()
		c := newClient(t, l.env, "172.18.0.5") // the web container
		c.headers = map[string]string{"X-Forwarded-For": xff, "Remote-User": user}
		c.must("POST", "/api/auth/proxy", nil, want)
		if want == 200 {
			c.must("GET", "/api/me", nil, 200)
		}
	}
	try("10.0.0.2", "pia", 404) // not set up yet

	l.settings(t, func(s *settings.Settings) {
		s.ProxyAuth.Enabled, s.ProxyAuth.Trusted = true, []string{"10.0.0.2/32"}
	})
	try("10.0.0.2", "pia", 200)
	try("192.168.1.99", "pia", 401)           // straight from a client
	try("10.0.0.2, 192.168.1.99", "pia", 401) // client claims to be behind the proxy
	try("10.0.0.2", "", 401)
	try("10.0.0.2", "nobody", 403)
}
