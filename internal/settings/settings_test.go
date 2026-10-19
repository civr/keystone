package settings

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

var requester = netip.MustParseAddr("192.168.1.5")

func TestDefaultsValidate(t *testing.T) {
	if err := Defaults().Validate(requester); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	if err := Defaults().Validate(netip.Addr{}); err != nil {
		t.Fatalf("defaults invalid for zero requester: %v", err)
	}
}

func assertField(t *testing.T, err error, field, contains string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	if ve.Field != field {
		t.Fatalf("field = %q, want %q", ve.Field, field)
	}
	if !strings.Contains(ve.Message, contains) {
		t.Fatalf("message %q lacks %q", ve.Message, contains)
	}
}

func TestRejectsAdminCIDRExcludingRequester(t *testing.T) {
	s := Defaults()
	s.Network.AdminCIDRs = []string{"10.0.0.0/24"}
	assertField(t, s.Validate(requester), "network.admin_cidrs", "buitensluiten")
}

func TestRejectsLoginCIDRExcludingRequester(t *testing.T) {
	s := Defaults()
	s.Network.LoginCIDRs = []string{"10.0.0.0/24"}
	assertField(t, s.Validate(requester), "network.login_cidrs", "buitensluiten")
}

func TestAcceptsCIDRContainingRequester(t *testing.T) {
	s := Defaults()
	s.Network.AdminCIDRs = []string{"10.0.0.0/24", "192.168.1.0/24"}
	s.Network.LoginCIDRs = []string{"192.168.0.0/16"}
	if err := s.Validate(requester); err != nil {
		t.Fatal(err)
	}
	// IPv4-mapped IPv6 requester still matches.
	if err := s.Validate(netip.MustParseAddr("::ffff:192.168.1.5")); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidRequesterSkipsLockoutCheck(t *testing.T) {
	s := Defaults()
	s.Network.AdminCIDRs = []string{"10.0.0.0/24"}
	if err := s.Validate(netip.Addr{}); err != nil {
		t.Fatal(err)
	}
}

func TestBounds(t *testing.T) {
	cases := []struct {
		field string
		mut   func(*Settings)
	}{
		{"session.duration_hours", func(s *Settings) { s.Session.DurationHours = 0 }},
		{"session.duration_hours", func(s *Settings) { s.Session.DurationHours = 8761 }},
		{"session.trusted_device_days", func(s *Settings) { s.Session.TrustedDeviceDays = 366 }},
		{"session.trusted_device_days", func(s *Settings) { s.Session.TrustedDeviceDays = -1 }},
		{"session.same_site", func(s *Settings) { s.Session.SameSite = "None" }},
		{"login.step_up_minutes", func(s *Settings) { s.Login.StepUpMinutes = 0 }},
		{"login.step_up_minutes", func(s *Settings) { s.Login.StepUpMinutes = 61 }},
		{"login.user_verification", func(s *Settings) { s.Login.UserVerification = "discouraged" }},
		{"brute_force.login_per_ip_per_minute", func(s *Settings) { s.BruteForce.LoginPerIPPerMinute = 1001 }},
		{"brute_force.login_per_account_per_minute", func(s *Settings) { s.BruteForce.LoginPerAccountPerMinute = 0 }},
		{"brute_force.lockout_threshold", func(s *Settings) { s.BruteForce.LockoutThreshold = 0 }},
		{"brute_force.lockout_minutes", func(s *Settings) { s.BruteForce.LockoutMinutes = 1001 }},
		{"network.login_cidrs", func(s *Settings) { s.Network.LoginCIDRs = []string{"nope"} }},
		{"network.trusted_proxies", func(s *Settings) { s.Network.TrustedProxies = []string{"999.1.1.1/8"} }},
		{"audit.syslog_proto", func(s *Settings) { s.Audit.SyslogProto = "icmp" }},
		{"public_url", func(s *Settings) { s.PublicURL = "ftp://x.lan" }},
		{"public_url", func(s *Settings) { s.PublicURL = "keystone.lan" }},
	}
	for _, c := range cases {
		s := Defaults()
		c.mut(&s)
		err := s.Validate(requester)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != c.field {
			t.Errorf("%s: got %v", c.field, err)
		}
	}
	s := Defaults()
	s.PublicURL = "https://keystone.home.lan"
	if err := s.Validate(requester); err != nil {
		t.Fatal(err)
	}
}
