package httpapi

import (
	"net/http"
	"net/netip"
	"strings"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/session"
)

// Signing in through an identity-aware reverse proxy (Settings › Sign-in &
// MFA › Reverse proxy). The proxy has already authenticated the person and
// passes their name in a header. That header is believed only when the
// request reached Keystone straight from a configured proxy address: the
// last X-Forwarded-For entry, which Caddy (the web container) always
// appends with the address it saw, so a client cannot forge it. Anyone
// else's copy of the header is ignored.

// proxyHop is the address that connected to Caddy.
func proxyHop(r *http.Request) netip.Addr {
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return netip.Addr{}
	}
	parts := strings.Split(xff[len(xff)-1], ",")
	ip, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil {
		return netip.Addr{}
	}
	return ip.Unmap()
}

func (s *server) authProxy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	p := cfg.ProxyAuth
	if !p.Enabled {
		writeError(w, http.StatusNotFound, "proxy_off", "Signing in through a proxy is not set up.")
		return
	}
	hop := proxyHop(r)
	trusted := false
	for _, pre := range parsePrefixes(p.Trusted) {
		trusted = trusted || (hop.IsValid() && pre.Contains(hop))
	}
	who := strings.TrimSpace(r.Header.Get(p.Header))
	if !trusted || who == "" {
		writeError(w, http.StatusUnauthorized, "proxy_unknown", "The proxy did not say who you are.")
		return
	}
	ip := clientIP(ctx)
	list, err := accounts.List(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "list accounts", err)
		return
	}
	var acct *accounts.Account
	for i, a := range list {
		key := a.Username
		if p.Match == "email" {
			key = a.Email
		}
		if key != "" && strings.EqualFold(key, who) {
			acct = &list[i]
		}
	}
	// The owner always signs in to Keystone itself: a proxy (or whoever
	// controls its address) can never become the owner.
	if acct == nil || acct.Disabled || acct.IsOwner() {
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"method": "proxy", "username": who})
		writeError(w, http.StatusForbidden, "not_allowed", "The proxy signed you in as someone Keystone does not know.")
		return
	}
	user := accountRef(acct.ID)
	passkeys, err := s.passkeyCount(ctx, user)
	if err != nil {
		errInternal(w, "count passkeys", err)
		return
	}
	// Not a confirmation: sensitive actions and the vault still ask.
	if !s.createSession(w, r, cfg, session.Session{UserID: user, NoPasskey: passkeys == 0}) {
		return
	}
	s.d.Audit.Log(ctx, actorOf(user), "login.success", ip, map[string]any{"method": "proxy"})
	writeJSON(w, http.StatusOK, map[string]string{"next": "done"})
}
