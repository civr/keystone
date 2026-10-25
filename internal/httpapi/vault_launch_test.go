package httpapi

import (
	"html"
	"strings"
	"testing"
)

// A vault app that signs in by form: a one-time launch posts the app's login
// form with the stored details, to the configured address only.
func TestVaultLaunch(t *testing.T) {
	t.Parallel()
	env, c := adminClient(t, "192.168.1.20")
	mk := func(mode string) string {
		var a map[string]any
		body(t, c.must("POST", "/api/apps", map[string]any{"name": "NAS", "url": "http://nas.home.lan", "color": "#007aff", "protocol": "vault",
			"config": map[string]any{"vault": map[string]string{"mode": mode, "login_url": "http://nas.home.lan/login", "user_field": "user", "pass_field": "pwd"}}}, 201).Body, &a)
		id := a["id"].(string)
		c.must("PUT", "/api/apps/"+id+"/vault", map[string]string{"username": "admin", "password": `p"w&d`}, 200)
		return id
	}
	c.must("POST", "/api/apps", map[string]any{"name": "Bad", "url": "http://x.home.lan", "color": "#007aff", "protocol": "vault",
		"config": map[string]any{"vault": map[string]string{"mode": "form", "login_url": "javascript:alert(1)"}}}, 400)

	form := mk("form")
	var l map[string]string
	body(t, c.must("POST", "/api/apps/"+form+"/vault/launch", nil, 200).Body, &l)
	w := c.must("GET", l["url"], nil, 200)
	page := html.UnescapeString(w.Body.String())
	if !strings.Contains(page, `action="http://nas.home.lan/login"`) || !strings.Contains(page, `name="user" value="admin"`) || !strings.Contains(page, `name="pwd" value="p"w&d"`) {
		t.Fatalf("form page:\n%s", page)
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action http://nas.home.lan;") {
		t.Fatalf("csp = %q", csp)
	}
	c.must("GET", l["url"], nil, 410) // used once

	js := mk("json")
	body(t, c.must("POST", "/api/apps/"+js+"/vault/launch", nil, 200).Body, &l)
	page = html.UnescapeString(c.must("GET", l["url"], nil, 200).Body.String())
	if !strings.Contains(page, `enctype="text/plain"`) || !strings.Contains(page, `name="{"pwd":"p\"w\u0026d","user":"admin","_":"" value=""}"`) {
		t.Fatalf("json page:\n%s", page)
	}

	body(t, c.must("POST", "/api/apps/"+js+"/vault/launch", nil, 200).Body, &l)
	if w := newClient(t, env.env, "192.168.1.40").do("GET", l["url"], nil); w.Code == 200 {
		t.Fatal("a launch works without the session that asked for it")
	}
}
