package realip

import (
	"net/netip"
	"testing"
)

func TestClientIPIgnoresSpoofedXFF(t *testing.T) {
	got := ClientIP("172.18.0.2:41234", "6.6.6.6, 203.0.113.9", nil)
	if want := netip.MustParseAddr("203.0.113.9"); got != want {
		t.Fatalf("ClientIP = %v, want %v", got, want)
	}
}

func TestClientIPSkipsTrustedProxies(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}
	got := ClientIP("172.18.0.2:41234", "203.0.113.9, 192.168.1.1", trusted)
	if want := netip.MustParseAddr("203.0.113.9"); got != want {
		t.Fatalf("ClientIP = %v, want %v", got, want)
	}
}

func TestClientIPEdgeCases(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}
	cases := []struct {
		name, remote, xff string
		want              string
	}{
		{"empty xff uses remote", "172.18.0.2:41234", "", "172.18.0.2"},
		{"remote without port", "172.18.0.2", "", "172.18.0.2"},
		{"ipv6 remote", "[2001:db8::1]:443", "", "2001:db8::1"},
		{"ipv4-mapped unmapped", "172.18.0.2:1", "::ffff:203.0.113.9", "203.0.113.9"},
		{"all trusted returns leftmost", "172.18.0.2:1", "192.168.1.7, 192.168.1.1", "192.168.1.7"},
		{"garbage left of trusted stops at trusted hop", "172.18.0.2:1", "nonsense, 192.168.1.1", "192.168.1.1"},
		{"multiple headers joined", "172.18.0.2:1", "6.6.6.6,203.0.113.9", "203.0.113.9"},
		{"whitespace-only xff uses remote", "172.18.0.2:1", "  ", "172.18.0.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClientIP(c.remote, c.xff, trusted)
			if got != netip.MustParseAddr(c.want) {
				t.Fatalf("ClientIP(%q, %q) = %v, want %s", c.remote, c.xff, got, c.want)
			}
		})
	}
}

func TestClientIPGarbageRemoteAndXFFIsInvalid(t *testing.T) {
	if got := ClientIP("not-an-ip", "", nil); got.IsValid() {
		t.Fatalf("ClientIP = %v, want invalid", got)
	}
}
