package apps

import "testing"

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		cfg      Config
		field    string
	}{
		{"link empty", "link", Config{}, ""},
		{"vault empty", "vault", Config{}, ""},
		{"unknown protocol", "kerberos", Config{}, "protocol"},
		{"saml ok", "saml", Config{SAML: &SAML{EntityID: "https://bastion.home.lan/api/auth/saml/metadata", ACSURL: "https://bastion.home.lan/api/auth/saml/acs"}}, ""},
		{"saml lan http ok", "saml", Config{SAML: &SAML{EntityID: "bastion", ACSURL: "http://bastion.home.lan/acs"}}, ""},
		{"saml public http", "saml", Config{SAML: &SAML{EntityID: "bastion", ACSURL: "http://bastion.example.com/acs"}}, "config.saml.acs_url"},
		{"saml missing", "saml", Config{}, "config.saml"},
		{"saml no entity", "saml", Config{SAML: &SAML{ACSURL: "https://b.lan/acs"}}, "config.saml.entity_id"},
		{"saml relative acs", "saml", Config{SAML: &SAML{EntityID: "x", ACSURL: "/acs"}}, "config.saml.acs_url"},
		{"oidc ok", "oidc", Config{OIDC: &OIDC{RedirectURIs: []string{"https://nimbus.home.lan/api/auth/sso/callback"}}}, ""},
		{"oidc none", "oidc", Config{OIDC: &OIDC{}}, "config.oidc.redirect_uris"},
		{"oidc fragment", "oidc", Config{OIDC: &OIDC{RedirectURIs: []string{"https://n.lan/cb#x"}}}, "config.oidc.redirect_uris"},
		{"oidc js", "oidc", Config{OIDC: &OIDC{RedirectURIs: []string{"javascript:alert(1)"}}}, "config.oidc.redirect_uris"},
		{"ldap ok", "ldap", Config{LDAP: &LDAP{Slug: "jellyfin"}}, ""},
		{"ldap bad slug", "ldap", Config{LDAP: &LDAP{Slug: "Jelly fin"}}, "config.ldap.slug"},
		{"gate ok", "gate", Config{Gate: &Gate{Host: "ha.home.lan", BypassPaths: []string{"/api/"}}}, ""},
		{"gate bad host", "gate", Config{Gate: &Gate{Host: "https://ha.home.lan"}}, "config.gate.host"},
		{"gate bad path", "gate", Config{Gate: &Gate{Host: "ha.home.lan", BypassPaths: []string{"api"}}}, "config.gate.bypass_paths"},
		{"account long", "link", Config{Account: Account{Username: string(make([]byte, 300))}}, "config.account.username"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := c.cfg
			field, msg := cfg.Validate(c.protocol)
			if field != c.field {
				t.Fatalf("field = %q (%s), want %q", field, msg, c.field)
			}
		})
	}
}

func TestValidateDropsOtherProtocols(t *testing.T) {
	cfg := Config{SAML: &SAML{EntityID: "x", ACSURL: "https://b.lan/acs"}, Gate: &Gate{Host: "x.lan"}}
	if f, _ := cfg.Validate("saml"); f != "" {
		t.Fatal(f)
	}
	if cfg.Gate != nil {
		t.Fatal("gate config kept on a saml app")
	}
}

func TestMappedFallsBackToOwner(t *testing.T) {
	o := Owner{Username: "thijs", DisplayName: "Thijs Willems", Email: "thijs@home.lan"}
	a := App{Config: Config{Account: Account{Username: "t.willems"}}}
	got := a.Mapped(o)
	want := Account{Username: "t.willems", Email: "thijs@home.lan", DisplayName: "Thijs Willems"}
	if got != want {
		t.Fatalf("got %+v", got)
	}
	if (App{}).Mapped(o).Username != "thijs" {
		t.Fatal("empty mapping should use owner")
	}
}
