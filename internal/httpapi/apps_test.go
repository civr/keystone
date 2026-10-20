package httpapi

import (
	"context"
	"encoding/base64"
	"testing"
	"time"
)

type appItem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
	Sort  int    `json:"sort"`
}

func listApps(t *testing.T, c *client) []appItem {
	t.Helper()
	var res struct {
		Apps []appItem `json:"apps"`
	}
	body(t, c.must("GET", "/api/apps", nil, 200).Body, &res)
	return res.Apps
}

func dataURL(mime, content string) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString([]byte(content))
}

func TestAppValidation(t *testing.T) {
	t.Parallel()
	_, c := adminClient(t, "192.168.1.20")
	ok := map[string]any{"name": "Jellyfin", "url": "https://jf.home.lan", "color": "#00A4DC"}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range ok {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	bad := map[string]map[string]any{
		"bad color":       with("color", "red"),
		"short color":     with("color", "#fff"),
		"javascript url":  with("url", "javascript:alert(1)"),
		"relative url":    with("url", "/foo"),
		"ftp url":         with("url", "ftp://x.lan"),
		"empty name":      with("name", ""),
		"long name":       with("name", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		"svg script":      with("icon", dataURL("image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)),
		"svg onload":      with("icon", dataURL("image/svg+xml", `<svg onload="x()"></svg>`)),
		"svg js href":     with("icon", dataURL("image/svg+xml", `<svg><a href="javascript:x()"><rect/></a></svg>`)),
		"icon wrong mime": with("icon", dataURL("text/html", "<b>")),
		"icon not base64": with("icon", "data:image/png;base64,!!!"),
		"icon http":       with("icon", "http://x/i.png"),
		"icon too big":    with("icon", dataURL("image/png", string(make([]byte, 64*1024)))),
	}
	for name, svg := range map[string]string{
		"svg entity":       `<svg>&#106;avascript:x</svg>`,
		"svg ENTITY":       `<!DOCTYPE svg [<!ENTITY x "y">]><svg>&x;</svg>`,
		"svg doctype":      `<!doctype svg><svg/>`,
		"svg prefixed":     `<svg xmlns:s="http://www.w3.org/2000/svg"><s:script>x</s:script></svg>`,
		"svg upper":        `<svg><SCRIPT>x</SCRIPT></svg>`,
		"svg foreign":      `<svg><foreignObject><div/></foreignObject></svg>`,
		"svg animate on":   `<svg><animate attributeName="onload" to="x()"/></svg>`,
		"svg animate on2":  `<svg><set attributeName='onclick' to='x()'/></svg>`,
		"svg onerror ws":   "<svg><image href=\"x\" OnError\t= \"x()\"/></svg>",
		"svg data uri":     `<svg><image href="data:text/html,x"/></svg>`,
		"svg iframe":       `<svg><iframe/></svg>`,
		"svg embed":        `<svg><embed/></svg>`,
		"svg object":       `<svg><object/></svg>`,
		"svg bom":          "\ufeff<svg/>",
		"svg nul":          "<svg>\x00</svg>",
		"svg invalid utf8": "<svg>\xff</svg>",
	} {
		bad[name] = with("icon", dataURL("image/svg+xml", svg))
	}
	for name, b := range bad {
		if w := c.do("POST", "/api/apps", b); w.Code != 400 || errCode(t, w) != "invalid" {
			t.Errorf("%s: POST = %d %s", name, w.Code, w.Body.String())
		}
	}
	if n := len(listApps(t, c)); n != 0 {
		t.Fatalf("%d apps stored from invalid requests", n)
	}
	good := with("icon", dataURL("image/svg+xml", `<svg xmlns="http://www.w3.org/2000/svg"><rect width="4" height="4"/></svg>`))
	var created appItem
	body(t, c.must("POST", "/api/apps", good, 201).Body, &created)
	if created.ID == "" || created.Name != "Jellyfin" || created.Icon == "" {
		t.Fatalf("created = %+v", created)
	}
	// Update + delete.
	c.must("PUT", "/api/apps/"+created.ID, with("name", "Media"), 200)
	if got := listApps(t, c); len(got) != 1 || got[0].Name != "Media" {
		t.Fatalf("after update: %+v", got)
	}
	if c.do("PUT", "/api/apps/"+created.ID, with("color", "nope")).Code != 400 {
		t.Fatal("invalid update accepted")
	}
	if c.do("PUT", "/api/apps/00000000-0000-0000-0000-000000000000", ok).Code != 404 {
		t.Fatal("unknown app not 404")
	}
	c.must("DELETE", "/api/apps/"+created.ID, nil, 200)
	if c.do("DELETE", "/api/apps/"+created.ID, nil).Code != 404 {
		t.Fatal("second delete not 404")
	}
}

func TestAppsReorder(t *testing.T) {
	t.Parallel()
	l, c := adminClient(t, "192.168.1.20")
	var ids []string
	for _, n := range []string{"A", "B", "C"} {
		var a appItem
		body(t, c.must("POST", "/api/apps", map[string]any{"name": n, "url": "https://" + n + ".home.lan", "color": "#112233"}, 201).Body, &a)
		ids = append(ids, a.ID)
	}
	names := func() string {
		s := ""
		for _, a := range listApps(t, c) {
			s += a.Name
		}
		return s
	}
	if names() != "ABC" {
		t.Fatalf("initial order %s", names())
	}
	c.must("PUT", "/api/apps/order", map[string]any{"ids": []string{ids[2], ids[0], ids[1]}}, 200)
	if names() != "CAB" {
		t.Fatalf("order %s", names())
	}
	for name, b := range map[string][]string{
		"missing":   {ids[0], ids[1]},
		"duplicate": {ids[0], ids[0], ids[1]},
		"unknown":   {ids[0], ids[1], "00000000-0000-0000-0000-000000000000"},
		"extra":     {ids[0], ids[1], ids[2], ids[2]},
		"not-uuid":  {ids[0], ids[1], "zzz"},
	} {
		if w := c.do("PUT", "/api/apps/order", map[string]any{"ids": b}); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if names() != "CAB" {
		t.Fatalf("order changed by invalid reorder: %s", names())
	}
	// Writes need step-up, reads do not.
	sess := l.session(t, c)
	sess.LastPasskeyAt = time.Now().Add(-2 * time.Hour)
	if err := l.deps.Sessions.Update(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if w := c.do("PUT", "/api/apps/order", map[string]any{"ids": ids}); w.Code != 403 || errCode(t, w) != "stepup_required" {
		t.Fatalf("reorder without step-up = %d", w.Code)
	}
	if len(listApps(t, c)) != 3 {
		t.Fatal("GET /apps needs no step-up")
	}
}
