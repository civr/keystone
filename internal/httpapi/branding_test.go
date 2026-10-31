package httpapi

import (
	"strings"
	"testing"
)

// The huisstijl is saved in Instellingen and lands in the shell of every
// page, the sign-in page included.
func TestBranding(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")

	var b map[string]string
	body(t, c.must("GET", "/api/admin/branding", nil, 200).Body, &b)
	if b["name"] != "Keystone" || b["accent"] != "#0f6cbd" || b["logo"] != "" {
		t.Fatalf("defaults = %v", b)
	}

	logo := dataURL("image/png", "\x89PNG\r\n\x1a\nfake")
	next := map[string]string{"name": "Huis <Willems>", "logo": logo, "accent": "#0A7A3F", "background": "#FAFAF7"}
	for field, bad := range map[string]map[string]string{
		"name":       {"name": " "},
		"accent":     {"accent": "#ffd60a"},
		"background": {"background": "#222222"},
		"logo":       {"logo": "data:text/html;base64,PHA+"},
	} {
		in := map[string]string{}
		for k, v := range next {
			in[k] = v
		}
		for k, v := range bad {
			in[k] = v
		}
		if w := c.do("PUT", "/api/admin/branding", in); w.Code != 400 || !strings.Contains(w.Body.String(), `"`+field+`"`) {
			t.Fatalf("%s: %d %s", field, w.Code, w.Body.String())
		}
	}
	c.must("PUT", "/api/admin/branding", next, 200)
	if n := l.audits(t, "branding.changed"); n != 1 {
		t.Fatalf("audits = %d", n)
	}

	anon := newClient(t, l.env, "192.168.1.30")
	page := anon.must("GET", "/login", nil, 200).Body.String()
	for _, want := range []string{
		"<title>Huis &lt;Willems&gt;</title>",
		`<meta name="ks-brand" content="Huis &lt;Willems&gt;" />`,
		`--primary:#0a7a3f;--color-bg:#fafaf7`,
		`<link rel="icon" href="` + logo + `" />`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("shell lacks %q:\n%s", want, page)
		}
	}
}
