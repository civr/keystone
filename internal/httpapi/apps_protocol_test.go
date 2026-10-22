package httpapi

import (
	"strings"
	"testing"
)

type protoApp struct {
	ID        string         `json:"id"`
	Protocol  string         `json:"protocol"`
	Config    map[string]any `json:"config"`
	HasSecret bool           `json:"has_secret"`
}

func oidcInput() map[string]any {
	return map[string]any{
		"name": "Nimbus", "url": "https://nimbus.home.lan", "color": "#0B5CFF", "protocol": "oidc",
		"config": map[string]any{
			"account": map[string]any{"username": "civr"},
			"oidc":    map[string]any{"redirect_uris": []string{"https://nimbus.home.lan/api/auth/sso/callback"}},
		},
	}
}

func TestAppProtocols(t *testing.T) {
	t.Parallel()
	_, c := adminClient(t, "192.168.1.20")

	var a protoApp
	body(t, c.must("POST", "/api/apps", oidcInput(), 201).Body, &a)
	oidc, _ := a.Config["oidc"].(map[string]any)
	clientID, _ := oidc["client_id"].(string)

	t.Run("CreateOIDCAppGetsClientID", func(t *testing.T) {
		if a.Protocol != "oidc" || len(clientID) != 36 || a.HasSecret {
			t.Fatalf("app = %+v", a)
		}
	})

	t.Run("SecretShownOnce", func(t *testing.T) {
		var res struct {
			Secret string `json:"secret"`
		}
		body(t, c.must("POST", "/api/apps/"+a.ID+"/secret", nil, 200).Body, &res)
		if len(res.Secret) < 40 {
			t.Fatalf("secret = %q", res.Secret)
		}
		w := c.must("GET", "/api/apps", nil, 200)
		if strings.Contains(w.Body.String(), res.Secret) || strings.Contains(w.Body.String(), "secret_hash") || strings.Contains(w.Body.String(), "argon2") {
			t.Fatalf("list leaks the secret: %s", w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"has_secret":true`) {
			t.Fatalf("has_secret missing: %s", w.Body.String())
		}
	})

	t.Run("UpdateKeepsSecretHashAndClientID", func(t *testing.T) {
		in := oidcInput()
		in["name"] = "Nimbus 2"
		var u protoApp
		body(t, c.must("PUT", "/api/apps/"+a.ID, in, 200).Body, &u)
		o, _ := u.Config["oidc"].(map[string]any)
		if !u.HasSecret || o["client_id"] != clientID {
			t.Fatalf("updated = %+v", u)
		}
	})

	t.Run("SecretOnlyForOIDCAndLDAP", func(t *testing.T) {
		var l protoApp
		body(t, c.must("POST", "/api/apps", map[string]any{"name": "Link", "url": "https://x.lan", "color": "#000000"}, 201).Body, &l)
		if l.Protocol != "link" {
			t.Fatalf("default protocol = %q", l.Protocol)
		}
		c.must("POST", "/api/apps/"+l.ID+"/secret", nil, 400)
	})

	t.Run("DuplicateEntityIDRejected", func(t *testing.T) {
		in := map[string]any{"name": "Bastion", "url": "https://bastion.home.lan", "color": "#107C10", "protocol": "saml",
			"config": map[string]any{"saml": map[string]any{"entity_id": "bastion", "acs_url": "https://bastion.home.lan/acs"}}}
		c.must("POST", "/api/apps", in, 201)
		w := c.must("POST", "/api/apps", in, 400)
		if !strings.Contains(w.Body.String(), "config.saml.entity_id") {
			t.Fatalf("body = %s", w.Body.String())
		}
	})

	t.Run("BadConfigRejected", func(t *testing.T) {
		in := oidcInput()
		in["config"] = map[string]any{"oidc": map[string]any{"redirect_uris": []string{}}}
		c.must("POST", "/api/apps", in, 400)
	})
}
