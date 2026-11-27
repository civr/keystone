package loginform

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Known is an app Keystone recognises, with the sign-in that works for it.
// Many self-hosted apps draw their login with JavaScript, so there is no
// form to find; for those the catalog below says how they log in.
type Known struct {
	Name string `json:"name"`
	// Protocol is the Keystone protocol to use: "gate" (keystone-gate logs
	// in server side), "vault" (the tile posts the login form) or "oidc"
	// (the app can sign in with Keystone itself).
	Protocol string `json:"protocol"`
	// Mode is the gate sign-in mode (form, json, jellyfin) or the vault mode
	// (form, json, copy).
	Mode      string `json:"mode"`
	LoginPath string `json:"login_path,omitempty"`
	UserField string `json:"user_field,omitempty"`
	PassField string `json:"pass_field,omitempty"`
	Note      string `json:"note,omitempty"`
}

type fingerprint struct {
	known Known
	// marker matches the app's own page; a first group, when there is one,
	// is the app's name (Sonarr, Radarr, …).
	marker *regexp.Regexp
	// probe is a public path that answers with probeRe when the page itself
	// gives nothing away (a bare single-page app).
	probe   string
	probeRe *regexp.Regexp
	// loginPage is the app's own login page, tried when the start page has
	// no form (the *arr apps let the local network in without one).
	loginPage string
}

const arrNote = "If Keystone finds no form, the app lets your home network in without a login (Settings › General › Authentication Required: Disabled for Local Addresses). Set it to Enabled so a login is asked, then try again."

// catalog lists the apps Keystone recognises. Order matters: the first match wins.
var catalog = []fingerprint{
	{
		known: Known{Name: "Jellyfin", Protocol: "gate", Mode: "jellyfin",
			Note: "Jellyfin keeps its login in the browser, which a tile can’t fill in. Put it behind the gate with the Jellyfin sign-in: the gate logs in for you and the web app opens signed in."},
		marker:  regexp.MustCompile(`(?i)<title>[^<]*Jellyfin|name="application-name"\s+content="Jellyfin"`),
		probe:   "/System/Info/Public",
		probeRe: regexp.MustCompile(`"ProductName"\s*:\s*"Jellyfin`),
	},
	{
		known:     Known{Name: "", Protocol: "vault", Mode: "form", LoginPath: "/login", UserField: "username", PassField: "password", Note: arrNote},
		marker:    regexp.MustCompile(`window\.(Sonarr|Radarr|Lidarr|Readarr|Prowlarr|Whisparr)\s*=|(?i)<title>[^<]*(Sonarr|Radarr|Lidarr|Readarr|Prowlarr|Whisparr)\b`),
		loginPage: "/login",
	},
	{
		known: Known{Name: "UniFi OS", Protocol: "gate", Mode: "json", LoginPath: "/api/auth/login", UserField: "username", PassField: "password",
			Note: "UniFi OS logs in through its API, so put it behind the gate with JSON login. Use a local admin account: a UI.com account with two-step login can’t be signed in for you."},
		marker: regexp.MustCompile(`(?i)<title>[^<]*UniFi OS|unifi-os|UNIFI_OS`),
	},
	{
		known: Known{Name: "UniFi Network", Protocol: "gate", Mode: "json", LoginPath: "/api/login", UserField: "username", PassField: "password",
			Note: "The UniFi Network application logs in through its API, so put it behind the gate with JSON login. Use a local admin account."},
		marker:  regexp.MustCompile(`(?i)<title>[^<]*UniFi Network|<title>[^<]*UniFi\s*</title>|ng-app="unifi`),
		probe:   "/status",
		probeRe: regexp.MustCompile(`"server_version"`),
	},
	{
		known: Known{Name: "Grafana", Protocol: "gate", Mode: "json", LoginPath: "/login", UserField: "user", PassField: "password",
			Note: "Grafana can also sign in with Keystone directly: choose OIDC and set up Generic OAuth in Grafana. That is the better choice; the gate with JSON login works too."},
		marker: regexp.MustCompile(`grafanaBootData|(?i)<title>[^<]*Grafana`),
	},
	{
		known: Known{Name: "", Protocol: "gate", Mode: "json", LoginPath: "/api/v1/auth/local", UserField: "email", PassField: "password",
			Note: "Put it behind the gate with JSON login, with the e-mail address of a local account as the username."},
		marker: regexp.MustCompile(`(?i)\b(Overseerr|Jellyseerr)\b`),
	},
	{
		known: Known{Name: "qBittorrent", Protocol: "gate", Mode: "form", LoginPath: "/api/v2/auth/login", UserField: "username", PassField: "password",
			Note: "qBittorrent only accepts a login from its own address, so put it behind the gate with the login form sign-in."},
		marker: regexp.MustCompile(`(?i)<title>[^<]*qBittorrent`),
	},
	{
		known: Known{Name: "Home Assistant", Protocol: "gate", Mode: "",
			Note: "Home Assistant keeps its login in the browser. Put it behind the gate; signing in for you needs Keystone’s Home Assistant component (header login)."},
		marker: regexp.MustCompile(`(?i)<title>[^<]*Home Assistant|<ha-launch-screen|home-assistant-main`),
	},
	{
		known: Known{Name: "Portainer", Protocol: "oidc", Mode: "copy",
			Note: "Portainer can sign in with Keystone itself: choose OIDC here and set up OAuth in Portainer (Settings › Authentication). Otherwise copy the password."},
		marker: regexp.MustCompile(`(?i)<title>[^<]*Portainer`),
	},
	{
		known: Known{Name: "Proxmox VE", Protocol: "vault", Mode: "copy",
			Note: "Proxmox VE logs in with a ticket its web app keeps itself, so copy the password. Proxmox can also sign in with Keystone via OpenID Connect (Datacenter › Permissions › Realms)."},
		marker: regexp.MustCompile(`(?i)Proxmox Virtual Environment|pve-manager|PVE\.Utils`),
	},
	{
		known: Known{Name: "Synology DSM", Protocol: "vault", Mode: "copy",
			Note: "DSM draws its login with JavaScript, so copy the password. DSM 7.2 and later can also sign in with Keystone via SSO Client › OpenID Connect."},
		marker: regexp.MustCompile(`(?i)<title>[^<]*Synology|SYNO\.SDS`),
	},
	{
		known:  Known{Name: "TrueNAS", Protocol: "vault", Mode: "copy", Note: "TrueNAS logs in over a WebSocket, which can’t be done for you. Copy the password."},
		marker: regexp.MustCompile(`(?i)<title>[^<]*TrueNAS`),
	},
}

// identify returns the known app the page belongs to, or nil. spa allows the
// probes, which only run for bare single-page apps.
func identify(ctx context.Context, c *http.Client, page []byte, base *url.URL, spa bool) (*Known, *fingerprint) {
	for i := range catalog {
		f := &catalog[i]
		if m := f.marker.FindSubmatch(page); m != nil {
			return named(f, m), f
		}
	}
	if !spa {
		return nil, nil
	}
	for i := range catalog {
		f := &catalog[i]
		if f.probe == "" {
			continue
		}
		u := *base
		u.Path, u.RawQuery, u.Fragment = f.probe, "", ""
		if body, ok := get(ctx, c, u.String(), 64<<10); ok && f.probeRe.Match(body) {
			k := f.known
			return &k, f
		}
	}
	return nil, nil
}

func named(f *fingerprint, m [][]byte) *Known {
	k := f.known
	if k.Name == "" {
		for _, g := range m[1:] {
			if len(g) > 0 {
				k.Name = canonical(string(g))
				break
			}
		}
	}
	return &k
}

// canonical spells a matched name the way the app does.
func canonical(s string) string {
	for _, n := range []string{"Sonarr", "Radarr", "Lidarr", "Readarr", "Prowlarr", "Whisparr", "Overseerr", "Jellyseerr"} {
		if strings.EqualFold(s, n) {
			return n
		}
	}
	return s
}

func get(ctx context.Context, c *http.Client, target string, limit int64) ([]byte, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Keystone)")
	resp, err := c.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return b, err == nil
}
