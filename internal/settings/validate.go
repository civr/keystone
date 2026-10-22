package settings

import (
	"fmt"
	"net/netip"
	"net/url"
)

// ValidationError identifies the offending field (JSON path) and a Dutch message.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

func bad(field, format string, a ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, a...)}
}

func inRange(field string, v, lo, hi int) error {
	if v < lo || v > hi {
		return bad(field, "moet tussen %d en %d liggen", lo, hi)
	}
	return nil
}

func oneOf(field, v string, allowed ...string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return bad(field, "moet een van deze waarden zijn: %v", allowed)
}

func parseCIDRs(field string, list []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(list))
	for _, c := range list {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, bad(field, "%q is geen geldige CIDR (bijv. 192.168.1.0/24)", c)
		}
		out = append(out, p)
	}
	return out, nil
}

func contains(prefixes []netip.Prefix, ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Validate checks bounds and formats. When requesterIP is valid and a
// login/admin CIDR list is non-empty, the list must contain the requester so
// the change cannot lock them out. An invalid requesterIP (e.g. CLI) skips
// that check.
func (s Settings) Validate(requesterIP netip.Addr) error {
	if s.PublicURL != "" {
		u, err := url.Parse(s.PublicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return bad("public_url", "moet een absolute http(s)-URL zijn")
		}
	}
	checks := []error{
		inRange("session.duration_hours", s.Session.DurationHours, 1, 8760),
		inRange("session.trusted_device_days", s.Session.TrustedDeviceDays, 0, 365),
		oneOf("session.same_site", s.Session.SameSite, "Lax", "Strict"),
		inRange("login.step_up_minutes", s.Login.StepUpMinutes, 1, 60),
		oneOf("login.user_verification", s.Login.UserVerification, "required", "preferred"),
		inRange("brute_force.login_per_ip_per_minute", s.BruteForce.LoginPerIPPerMinute, 1, 1000),
		inRange("brute_force.login_per_account_per_minute", s.BruteForce.LoginPerAccountPerMinute, 1, 1000),
		inRange("brute_force.lockout_threshold", s.BruteForce.LockoutThreshold, 1, 1000),
		inRange("brute_force.lockout_minutes", s.BruteForce.LockoutMinutes, 1, 1000),
		oneOf("audit.syslog_proto", s.Audit.SyslogProto, "udp", "tcp", "tls"),
		inRange("tokens.access_minutes", s.Tokens.AccessMinutes, 1, 1440),
		inRange("tokens.id_minutes", s.Tokens.IDMinutes, 1, 1440),
		inRange("tokens.refresh_days", s.Tokens.RefreshDays, 0, 365),
		inRange("ldap.bind_per_ip_per_minute", s.LDAP.BindPerIPPerMinute, 1, 1000),
		inRange("gate.fail_per_ip_per_minute", s.Gate.FailPerIPPerMinute, 1, 10000),
		inRange("vault.reconfirm_minutes", s.Vault.ReconfirmMinutes, 1, 1440),
		inRange("vault.clipboard_clear_seconds", s.Vault.ClipboardClearSeconds, 5, 600),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	login, err := parseCIDRs("network.login_cidrs", s.Network.LoginCIDRs)
	if err != nil {
		return err
	}
	admin, err := parseCIDRs("network.admin_cidrs", s.Network.AdminCIDRs)
	if err != nil {
		return err
	}
	if _, err := parseCIDRs("ldap.allowed_cidrs", s.LDAP.AllowedCIDRs); err != nil {
		return err
	}
	if _, err := parseCIDRs("network.trusted_proxies", s.Network.TrustedProxies); err != nil {
		return err
	}
	if requesterIP.IsValid() {
		if len(admin) > 0 && !contains(admin, requesterIP) {
			return bad("network.admin_cidrs", "dit zou je eigen adres (%s) buitensluiten van de beheerpagina", requesterIP.Unmap())
		}
		if len(login) > 0 && !contains(login, requesterIP) {
			return bad("network.login_cidrs", "dit zou je eigen adres (%s) buitensluiten van inloggen", requesterIP.Unmap())
		}
	}
	return nil
}
