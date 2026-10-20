package ad

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	ok := Config{URL: "ldaps://dc.home.lan", BindDN: "cn=svc,dc=home,dc=lan", BaseDN: "dc=home,dc=lan"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for name, mut := range map[string]func(*Config){
		"no url":          func(c *Config) { c.URL = "" },
		"bad scheme":      func(c *Config) { c.URL = "http://dc.home.lan" },
		"no host":         func(c *Config) { c.URL = "ldaps://" },
		"plaintext":       func(c *Config) { c.URL = "ldap://dc.home.lan" },
		"no bind dn":      func(c *Config) { c.BindDN = "" },
		"no base dn":      func(c *Config) { c.BaseDN = " " },
		"filter no %s":    func(c *Config) { c.UserFilter = "(objectClass=user)" },
		"min tls":         func(c *Config) { c.MinTLS = "1.1" },
		"bad ca":          func(c *Config) { c.CAPEM = "not a pem" },
		"timeout too big": func(c *Config) { c.TimeoutSeconds = 61 },
		"timeout neg":     func(c *Config) { c.TimeoutSeconds = -1 },
	} {
		c := ok
		mut(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, mut := range map[string]func(*Config){
		"starttls":        func(c *Config) { c.URL = "ldap://dc.home.lan"; c.StartTLS = true },
		"allow plaintext": func(c *Config) { c.URL = "ldap://dc.home.lan:389"; c.AllowPlaintext = true },
		"min tls 1.3":     func(c *Config) { c.MinTLS = "1.3" },
		"custom filter":   func(c *Config) { c.UserFilter = "(&(objectClass=person)(uid=%s))" },
	} {
		c := ok
		mut(&c)
		if err := c.Validate(); err != nil {
			t.Errorf("%s: rejected: %v", name, err)
		}
	}
}

func TestConfigJSONIsSnakeCase(t *testing.T) {
	raw, err := json.Marshal(Config{URL: "ldaps://x", StartTLS: true, BindPasswordSealed: "s", TimeoutSeconds: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"url"`, `"start_tls"`, `"insecure_skip_verify"`, `"ca_pem"`, `"bind_dn"`, `"bind_password_sealed"`,
		`"base_dn"`, `"user_filter"`, `"min_tls"`, `"timeout_seconds"`, `"allow_plaintext"`} {
		if !strings.Contains(string(raw), k) {
			t.Errorf("JSON %s lacks %s", raw, k)
		}
	}
}
