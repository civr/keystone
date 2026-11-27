package loginform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serve(t *testing.T, routes map[string]string) string {
	t.Helper()
	mux := http.NewServeMux()
	for path, body := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if path == "/" && r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(body))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

const spa = `<!doctype html><html><head><script type="module" src="/a.js"></script></head><body><div id="app"></div></body></html>`

func TestKnownApps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		routes map[string]string
		app    string
		mode   string
		form   bool
	}{
		{"jellyfin by title", map[string]string{"/": `<html><head><title>Jellyfin</title><script src="main.js"></script></head><body></body></html>`}, "Jellyfin", "jellyfin", false},
		{"jellyfin by probe", map[string]string{"/": spa, "/System/Info/Public": `{"LocalAddress":"x","ProductName":"Jellyfin Server","Version":"10.11.0"}`}, "Jellyfin", "jellyfin", false},
		{"sonarr open to the lan", map[string]string{"/": `<html><head><title>Sonarr</title></head><body><div id="root"></div><script>window.Sonarr = {apiRoot:'/api/v3'}</script><script src="a.js"></script><script src="b.js"></script></body></html>`}, "Sonarr", "form", false},
		{"radarr login page", map[string]string{
			"/":      `<html><head><title>Radarr</title></head><body><div id="root"></div><script>window.Radarr = {}</script><script src="a.js"></script><script src="b.js"></script></body></html>`,
			"/login": `<html><head><title>Login - Radarr</title></head><body><form method="POST"><input type="email" name="username"><input type="password" name="password"><input type="checkbox" name="rememberMe" checked></form></body></html>`,
		}, "Radarr", "form", true},
		{"unifi os", map[string]string{"/": `<!doctype html><html><head><title>UniFi OS</title><script type="module" src="/x.js"></script></head><body><div id="root"></div></body></html>`}, "UniFi OS", "json", false},
		{"unifi network by probe", map[string]string{"/": spa, "/status": `{"meta":{"rc":"ok","server_version":"9.0.114","up":true}}`}, "UniFi Network", "json", false},
		{"grafana", map[string]string{"/": `<html><head><title>Grafana</title></head><body><script>window.grafanaBootData = {}</script></body></html>`}, "Grafana", "json", false},
		{"jellyseerr", map[string]string{"/": `<html><head><title>Jellyseerr</title></head><body><div id="__next"></div><script id="__NEXT_DATA__"></script></body></html>`}, "Jellyseerr", "json", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p, err := Fetch(context.Background(), NewClient(5*time.Second), serve(t, c.routes)+"/")
			if err != nil {
				t.Fatal(err)
			}
			if p.App == nil || p.App.Name != c.app || p.App.Mode != c.mode {
				t.Fatalf("app = %+v", p.App)
			}
			if (p.Form != nil) != c.form {
				t.Fatalf("form = %+v", p.Form)
			}
			if c.form && (p.Form.UserField != "username" || p.Form.Extra[0] != (Field{"rememberMe", "on"})) {
				t.Fatalf("form = %+v", p.Form)
			}
		})
	}
}

func TestUnknownApp(t *testing.T) {
	t.Parallel()
	p, err := Fetch(context.Background(), NewClient(5*time.Second), serve(t, map[string]string{"/": spa})+"/")
	if err != nil || p.App != nil || p.Form != nil || !p.Script {
		t.Fatalf("p = %+v, err = %v", p, err)
	}
}
