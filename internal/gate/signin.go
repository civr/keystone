package gate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/gatetoken"
	"github.com/civr/keystone/internal/session"
)

// Signing in to a gate app for you (apps.GateSignIn). The credentials are the
// ones in the vault. basic and header modes add a request header in /verify;
// form, json and jellyfin log the browser in at /.keystone/signin, where the
// gate calls the app's login itself, so no password reaches the browser and
// apps whose login is an API call (Jellyfin) work too.

const (
	credsTTL     = 30 * time.Second
	signinTTL    = 12 * time.Hour
	maxLoginBody = 1 << 20
	signinPath   = "/.keystone/"
)

type creds struct {
	user, pass string
	ok         bool
	at         time.Time
}

// credsCache keeps vault entries for a few seconds so a page full of
// requests does not hit the database and decrypt each time.
type credsCache struct {
	mu sync.Mutex
	m  map[string]creds
}

// credentials returns the stored username and password of app; ok is false
// when no password is stored.
func (s *Server) credentials(ctx context.Context, appID string) (user, pass string, ok bool, err error) {
	s.cache.mu.Lock()
	c, hit := s.cache.m[appID]
	s.cache.mu.Unlock()
	if hit && time.Since(c.at) < credsTTL {
		return c.user, c.pass, c.ok, nil
	}
	var sealed *string
	c = creds{at: time.Now()}
	err = s.Pool.QueryRow(ctx, `select username, password_sealed from vault_entries where app_id = $1`, appID).Scan(&c.user, &sealed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, err
	}
	if err == nil && sealed != nil {
		pw, err := crypto.Open(s.VaultKey, *sealed)
		if err != nil {
			return "", "", false, err
		}
		c.pass, c.ok = string(pw), true
	}
	s.cache.mu.Lock()
	if s.cache.m == nil {
		s.cache.m = map[string]creds{}
	}
	s.cache.m[appID] = c
	s.cache.mu.Unlock()
	return c.user, c.pass, c.ok, nil
}

// marker is the host-only cookie that says the browser was signed in to the
// app, so /verify does not send it to /.keystone/signin on every visit.
func marker(appID string) string {
	return "ks_signed_" + strings.ReplaceAll(appID, "-", "")[:8]
}

// addHeaders sets the header the app reads its login from (basic, header).
func (s *Server) addHeaders(ctx context.Context, h http.Header, app *apps.App) error {
	si := app.Config.Gate.SignIn
	if si == nil || (si.Mode != apps.SignInBasic && si.Mode != apps.SignInHeader) {
		return nil
	}
	user, pass, ok, err := s.credentials(ctx, app.ID)
	if err != nil || !ok {
		return err
	}
	if si.Mode == apps.SignInBasic {
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	} else {
		h.Set(si.Header, pass)
	}
	return nil
}

// wantsSignIn reports whether a top-level page load should first pass
// /.keystone/signin: the app logs in server side, the browser is not signed
// in yet and there is a password to sign in with.
// A closed vault lets the page through to the app's own login: sign-in
// would only stop at "Confirm it is you", even when the app's session is live.
func (s *Server) wantsSignIn(r *http.Request, u *url.URL, app *apps.App, sess session.Session, window time.Duration) (bool, error) {
	if !app.Config.Gate.SignIn.Server() || r.URL.Query().Get("mode") == "nginx" || strings.HasPrefix(u.Path, signinPath) {
		return false, nil
	}
	if !session.VaultOpen(sess, window, time.Now()) {
		return false, nil
	}
	method := r.Header.Get("X-Forwarded-Method")
	if method == "" {
		method = r.Method
	}
	if method != http.MethodGet || r.Header.Get("Sec-Fetch-Mode") != "navigate" || r.Header.Get("Sec-Fetch-Dest") != "document" {
		return false, nil
	}
	if _, err := r.Cookie(marker(app.ID)); err == nil {
		return false, nil
	}
	_, _, ok, err := s.credentials(r.Context(), app.ID)
	return ok, err
}

func signInURL(u *url.URL) string {
	return u.Scheme + "://" + u.Host + signinPath + "signin?rd=" + url.QueryEscape(u.RequestURI())
}

// forwardedHost is the app host the browser asked the proxy for.
func forwardedHost(r *http.Request) string {
	h := r.Header.Get("X-Forwarded-Host")
	if h == "" {
		h = r.Host
	}
	h, _, _ = strings.Cut(h, ",")
	h = strings.TrimSpace(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(h)
}

// safeRD reports whether rd is a path on the app's own host. Browsers drop
// tabs and newlines and read a backslash as /, so "/\t/evil" would leave the host.
func safeRD(rd string) bool {
	for i := 0; i < len(rd); i++ {
		if rd[i] < 0x20 || rd[i] == 0x7f || rd[i] == '\\' {
			return false
		}
	}
	return strings.HasPrefix(rd, "/") && !strings.HasPrefix(rd, "//")
}

type noticePage struct {
	Title, Text, Link, LinkText string
}

func notice(w http.ResponseWriter, status int, p noticePage) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_ = noticeTmpl.Execute(w, p)
}

var noticeTmpl = template.Must(template.New("notice").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>{{.Title}}</title></head>
<body style="font-family:system-ui,sans-serif;color:#6e6e73;display:grid;place-items:center;min-height:90vh;text-align:center">
<div><p>{{.Text}}</p><p><a href="{{.Link}}">{{.LinkText}}</a></p></div>
</body></html>`))

// signIn logs the browser in at the app with the stored credentials.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "cross-site request", http.StatusForbidden)
		return
	}
	v, err := s.View.Get(ctx)
	if err != nil {
		slog.Error("gate: load view", "err", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	host := forwardedHost(r)
	var app *apps.App
	for i, a := range v.Apps {
		if a.Protocol == apps.GateP && a.Config.Gate != nil && a.Config.Gate.Host == host && a.Config.Gate.SignIn.Server() {
			app = &v.Apps[i]
		}
	}
	if app == nil {
		http.Error(w, "unknown app", http.StatusForbidden)
		return
	}
	si := app.Config.Gate.SignIn
	rd := r.URL.Query().Get("rd")
	if rd == "" {
		rd = "/"
	}
	if !safeRD(rd) {
		http.Error(w, "bad redirect", http.StatusBadRequest)
		return
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto != "http" {
		proto = "https"
	}
	c, err := r.Cookie(gatetoken.Cookie)
	if err != nil {
		http.Redirect(w, r, startURL(v.Settings.PublicURL, proto+"://"+host+signinPath+"signin?rd="+url.QueryEscape(rd)), http.StatusFound)
		return
	}
	sess, err := gatetoken.Resolve(ctx, s.Valkey, s.Sessions, v.Settings, c.Value, time.Now())
	if errors.Is(err, gatetoken.ErrNotFound) {
		http.Redirect(w, r, startURL(v.Settings.PublicURL, proto+"://"+host+signinPath+"signin?rd="+url.QueryEscape(rd)), http.StatusFound)
		return
	}
	if err != nil {
		slog.Error("gate: resolve", "err", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	who, ok := v.Account(sess.UserID)
	if !ok || !app.AllowedFor(who) || (app.Config.RequireMFA && !sess.MFA()) {
		http.Error(w, "no access to this app", http.StatusForbidden)
		return
	}
	window := time.Duration(v.Settings.Vault.ReconfirmMinutes) * time.Minute
	if !session.VaultOpen(sess, window, time.Now()) {
		notice(w, http.StatusForbidden, noticePage{Title: "Confirm it is you",
			Text: "Open " + app.Name + " from Keystone to confirm it is you.", Link: v.Settings.PublicURL, LinkText: "Go to Keystone"})
		return
	}
	if ok, err := s.Limiter.Allow(ctx, "gate-ip", clientIP(r), v.Settings.Gate.FailPerIPPerMinute); err == nil && !ok {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	user, pass, has, err := s.credentials(ctx, app.ID)
	if err != nil {
		slog.Error("gate: credentials", "err", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if !has {
		notice(w, http.StatusNotFound, noticePage{Title: "No password", Text: "No password is stored for " + app.Name + ".", Link: rd, LinkText: "Continue to " + app.Name})
		return
	}
	mark := &http.Cookie{Name: marker(app.ID), Value: "1", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: proto == "https"}
	fail := func(status int) {
		slog.Info("gate: sign-in failed", "app", app.Name, "status", status)
		// Without a marker the link below would only come back here.
		mark.MaxAge = 300
		http.SetCookie(w, mark)
		why := "the app did not answer"
		if status > 0 {
			why = "status " + strconv.Itoa(status)
		}
		notice(w, http.StatusBadGateway, noticePage{Title: "Sign-in failed",
			Text: "Keystone could not sign you in to " + app.Name + " (" + why + ").", Link: rd, LinkText: "Continue to " + app.Name})
	}
	var status int
	var cookies []*http.Cookie
	var jf *jellyfinAuth
	switch si.Mode {
	case apps.SignInJellyfin:
		device := deviceID(sess.ID, app.ID)
		status, jf, err = s.jellyfinLogin(ctx, si.Upstream, device, user, pass)
		if err == nil && jf == nil {
			err = errors.New("no token")
		}
	default:
		status, cookies, err = s.formLogin(ctx, si, user, pass, proto == "https")
	}
	if err != nil {
		slog.Warn("gate: sign-in", "app", app.Name, "err", err)
		fail(status)
		return
	}
	if s.Audit != nil {
		ip, _ := netip.ParseAddr(clientIP(r))
		s.Audit.Log(ctx, actorOf(sess.UserID), "vault.revealed", ip, map[string]any{"id": app.ID, "app": app.Name, "method": "gate"})
	}
	mark.MaxAge = int(signinTTL / time.Second)
	http.SetCookie(w, mark)
	if jf != nil {
		serveJellyfin(w, jf, rd)
		return
	}
	for _, c := range cookies {
		http.SetCookie(w, c)
	}
	http.Redirect(w, r, rd, http.StatusFound)
}

func actorOf(user string) string {
	if user == "" {
		return audit.ActorOwner
	}
	return "user:" + user
}

var upstream = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// formLogin posts the login form (or JSON body) to the app and returns the
// cookies it sets, made host-only for the browser. The status is the app's.
// The app is reached over plain http inside, so on an https host the cookies
// are made Secure here.
func (s *Server) formLogin(ctx context.Context, si *apps.GateSignIn, user, pass string, secure bool) (int, []*http.Cookie, error) {
	var body, ctype string
	if si.Mode == apps.SignInJSON {
		raw, _ := json.Marshal(map[string]string{si.UserField: user, si.PassField: pass})
		body, ctype = string(raw), "application/json"
	} else {
		body, ctype = url.Values{si.UserField: {user}, si.PassField: {pass}}.Encode(), "application/x-www-form-urlencoded"
	}
	resp, err := post(ctx, si.Upstream+si.LoginPath, ctype, body, nil)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxLoginBody))
	set := resp.Cookies()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 || len(set) == 0 {
		return resp.StatusCode, nil, errors.New("login refused")
	}
	out := make([]*http.Cookie, 0, len(set))
	for _, c := range set {
		path := c.Path
		if path == "" {
			path = "/"
		}
		out = append(out, &http.Cookie{
			Name: c.Name, Value: c.Value, Path: path, Expires: c.Expires, MaxAge: c.MaxAge,
			HttpOnly: c.HttpOnly, Secure: c.Secure || secure, SameSite: c.SameSite, Quoted: c.Quoted,
		})
	}
	return resp.StatusCode, out, nil
}

func post(ctx context.Context, target, ctype, body string, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ctype)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return upstream.Do(req)
}

type jellyfinAuth struct {
	Device, Token, User, Server string
}

// deviceID names this browser's sign-in to Jellyfin: stable per session and app.
func deviceID(sessionID, appID string) string {
	sum := sha256.Sum256([]byte(sessionID + appID))
	return "keystone-" + hex.EncodeToString(sum[:])[:16]
}

// jellyfinLogin authenticates by name at Jellyfin.
func (s *Server) jellyfinLogin(ctx context.Context, base, device, user, pass string) (int, *jellyfinAuth, error) {
	body, _ := json.Marshal(map[string]string{"Username": user, "Pw": pass})
	auth := `MediaBrowser Client="Keystone", Device="Keystone", DeviceId="` + device + `", Version="1.0"`
	resp, err := post(ctx, base+"/Users/AuthenticateByName", "application/json", string(body), map[string]string{"Authorization": auth})
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxLoginBody))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil, errors.New("login refused")
	}
	var out struct {
		AccessToken string
		ServerId    string
		User        struct{ Id string }
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" || out.User.Id == "" {
		return resp.StatusCode, nil, errors.New("unexpected login answer")
	}
	return resp.StatusCode, &jellyfinAuth{Device: device, Token: out.AccessToken, User: out.User.Id, Server: out.ServerId}, nil
}

// The page hands Jellyfin's web client its sign-in: it reads these two keys
// from localStorage of the app's origin, which only a page there can set.
const jellyfinScript = `var d=JSON.parse(document.getElementById('ks').textContent);
try{localStorage.setItem('_deviceId2',d.device);
var k='jellyfin_credentials',c={};
try{c=JSON.parse(localStorage.getItem(k))||{}}catch(e){}
var l=(c.Servers||[]).filter(function(s){return s.Id!==d.server});
l.unshift({Id:d.server,AccessToken:d.token,UserId:d.user,ManualAddress:location.origin,LocalAddress:location.origin,DateLastAccessed:Date.now(),LastConnectionMode:2});
c.Servers=l;localStorage.setItem(k,JSON.stringify(c))}catch(e){}
location.replace(d.next)`

func jellyfinScriptHash() string {
	sum := sha256.Sum256([]byte(jellyfinScript))
	return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
}

var jellyfinTmpl = template.Must(template.New("jellyfin").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="robots" content="noindex"><title>Signing in…</title></head>
<body style="font-family:system-ui,sans-serif;color:#6e6e73;display:grid;place-items:center;min-height:90vh">
<p>Signing in…</p>
<script type="application/json" id="ks">{{.}}</script>
<script>` + jellyfinScript + `</script>
</body></html>`))

func serveJellyfin(w http.ResponseWriter, a *jellyfinAuth, rd string) {
	next := rd
	if rd == "/" {
		next = "/web/index.html"
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; script-src '"+jellyfinScriptHash()+"'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	var buf bytes.Buffer
	_ = jellyfinTmpl.Execute(&buf, map[string]string{"device": a.Device, "token": a.Token, "user": a.User, "server": a.Server, "next": next})
	_, _ = w.Write(buf.Bytes())
}
