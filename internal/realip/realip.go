// Package realip derives the client IP from RemoteAddr and X-Forwarded-For
// without trusting addresses a client could have supplied itself.
package realip

import (
	"net"
	"net/netip"
	"strings"
)

// ClientIP returns the address of the client. RemoteAddr (Caddy) is always a
// trusted hop. X-Forwarded-For is walked right to left: entries inside
// trusted are proxies and skipped; the first untrusted entry is the client.
// If every entry is trusted the leftmost one is returned; an unparseable
// entry stops the walk at the last hop that was vouched for. With an empty
// X-Forwarded-For the RemoteAddr host is returned. All results are unmapped
// (::ffff:a.b.c.d → a.b.c.d); the zero Addr means no usable address.
func ClientIP(remoteAddr, xff string, trusted []netip.Prefix) netip.Addr {
	client := parseRemote(remoteAddr)
	hops := strings.Split(xff, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		if h == "" {
			continue
		}
		ip, err := netip.ParseAddr(h)
		if err != nil {
			return client
		}
		client = ip.Unmap().WithZone("")
		if !inAny(trusted, client) {
			return client
		}
	}
	return client
}

func parseRemote(remoteAddr string) netip.Addr {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip.Unmap().WithZone("")
}

func inAny(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
