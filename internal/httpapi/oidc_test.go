package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const nimbusCB = "https://nimbus.home.lan/api/auth/sso/callback"

type oidcFixture struct {
	l                       *loginEnv
	c                       *client
	clientID, secret, appID string
}

func newOIDC(t *testing.T) *oidcFixture {
	l, c := adminClient(t, "192.168.1.20")
	in := oidcInput()
	in["config"].(map[string]any)["account"] = map[string]any{"username": "civr", "email": "civr@home.lan"}
	var a protoApp
	body(t, c.must("POST", "/api/apps", in, 201).Body, &a)
	var sec struct {
		Secret string `json:"secret"`
	}
	body(t, c.must("POST", "/api/apps/"+a.ID+"/secret", nil, 200).Body, &sec)
	return &oidcFixture{l: l, c: c, clientID: a.Config["oidc"].(map[string]any)["client_id"].(string), secret: sec.Secret, appID: a.ID}
}

const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (f *oidcFixture) authorizeURL(mut func(url.Values)) string {
	q := url.Values{
		"response_type": {"code"}, "client_id": {f.clientID}, "redirect_uri": {nimbusCB},
		"scope": {"openid profile email offline_access"}, "state": {"st"}, "nonce": {"n0nce"},
		"code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"},
	}
	if mut != nil {
		mut(q)
	}
	return "/oidc/authorize?" + q.Encode()
}

// code runs the authorize step and returns the code from the redirect.
func (f *oidcFixture) code(t *testing.T) string {
	t.Helper()
	w := f.c.must("GET", f.authorizeURL(nil), nil, 302)
	loc, _ := url.Parse(w.Header().Get("Location"))
	if !strings.HasPrefix(loc.String(), nimbusCB+"?") || loc.Query().Get("state") != "st" {
		t.Fatalf("location = %s", loc)
	}
	return loc.Query().Get("code")
}

// token posts to the token endpoint like a server would: no Origin, no cookies.
func (f *oidcFixture) token(form url.Values, basic bool) *httptest.ResponseRecorder {
	if basic {
		form.Del("client_secret")
	}
	r := httptest.NewRequest("POST", "/oidc/token", strings.NewReader(form.Encode()))
	r.Host = setupHost
	r.RemoteAddr = "192.168.1.30:5000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basic {
		r.SetBasicAuth(url.QueryEscape(f.clientID), url.QueryEscape(f.secret))
	}
	w := httptest.NewRecorder()
	f.l.h.ServeHTTP(w, r)
	return w
}

func (f *oidcFixture) exchange(code, v string) *httptest.ResponseRecorder {
	return f.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {nimbusCB},
		"code_verifier": {v}, "client_id": {f.clientID}, "client_secret": {f.secret}}, false)
}

func TestOIDC(t *testing.T) {
	t.Parallel()
	f := newOIDC(t)
	c := f.c

	t.Run("Discovery", func(t *testing.T) {
		var d map[string]any
		body(t, c.must("GET", "/.well-known/openid-configuration", nil, 200).Body, &d)
		if d["issuer"] != setupOrigin || d["token_endpoint"] != setupOrigin+"/oidc/token" {
			t.Fatalf("discovery = %v", d)
		}
	})

	t.Run("AuthorizeRedirectURIMismatchNoRedirect", func(t *testing.T) {
		w := c.do("GET", f.authorizeURL(func(q url.Values) { q.Set("redirect_uri", "https://evil.example/cb") }), nil)
		if w.Code != 400 || w.Header().Get("Location") != "" {
			t.Fatalf("got %d %q", w.Code, w.Header().Get("Location"))
		}
		w = c.do("GET", f.authorizeURL(func(q url.Values) { q.Set("client_id", "nope") }), nil)
		if w.Code != 400 || w.Header().Get("Location") != "" {
			t.Fatalf("unknown client: %d", w.Code)
		}
	})

	t.Run("AuthorizeRequiresPKCE", func(t *testing.T) {
		w := c.must("GET", f.authorizeURL(func(q url.Values) { q.Del("code_challenge") }), nil, 302)
		loc, _ := url.Parse(w.Header().Get("Location"))
		if loc.Query().Get("error") != "invalid_request" || loc.Query().Get("code") != "" {
			t.Fatalf("location = %s", loc)
		}
	})

	t.Run("NoSessionRedirectsToLogin", func(t *testing.T) {
		anon := newClient(t, f.l.env, "192.168.1.22")
		w := anon.must("GET", f.authorizeURL(nil), nil, 302)
		if !strings.HasPrefix(w.Header().Get("Location"), "/login?next=%2Foidc%2Fauthorize%3F") {
			t.Fatalf("location = %q", w.Header().Get("Location"))
		}
	})

	var refresh string
	t.Run("CodeFlowIDTokenVerifiesWithJWKS", func(t *testing.T) {
		w := f.exchange(f.code(t), verifier)
		if w.Code != 200 {
			t.Fatalf("token = %d %s", w.Code, w.Body.String())
		}
		var tr struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		body(t, w.Body, &tr)
		refresh = tr.RefreshToken
		var set jose.JSONWebKeySet
		body(t, c.must("GET", "/oidc/jwks", nil, 200).Body, &set)
		tok, err := jwt.ParseSigned(tr.IDToken, []jose.SignatureAlgorithm{jose.RS256})
		if err != nil {
			t.Fatal(err)
		}
		keys := set.Key(tok.Headers[0].KeyID)
		if len(keys) != 1 {
			t.Fatalf("kid %q not in jwks", tok.Headers[0].KeyID)
		}
		var std jwt.Claims
		var ext struct {
			Nonce    string `json:"nonce"`
			Username string `json:"preferred_username"`
			Email    string `json:"email"`
		}
		if err := tok.Claims(keys[0].Key, &std, &ext); err != nil {
			t.Fatal(err)
		}
		if err := std.Validate(jwt.Expected{Issuer: setupOrigin, AnyAudience: jwt.Audience{f.clientID}}); err != nil {
			t.Fatal(err)
		}
		if ext.Nonce != "n0nce" || ext.Username != "civr" || ext.Email != "civr@home.lan" {
			t.Fatalf("claims = %+v", ext)
		}
		r := httptest.NewRequest("GET", "/oidc/userinfo", nil)
		r.Host = setupHost
		r.RemoteAddr = "192.168.1.30:5000"
		r.Header.Set("Authorization", "Bearer "+tr.AccessToken)
		uw := httptest.NewRecorder()
		f.l.h.ServeHTTP(uw, r)
		if uw.Code != 200 || !strings.Contains(uw.Body.String(), `"preferred_username":"civr"`) {
			t.Fatalf("userinfo = %d %s", uw.Code, uw.Body.String())
		}
	})

	t.Run("AMRFollowsSession", func(t *testing.T) {
		sess := f.l.session(t, c)
		saved := sess.LastPasskeyAt
		sess.LastPasskeyAt = time.Time{} // signed in with the password only
		if err := f.l.deps.Sessions.Update(context.Background(), sess); err != nil {
			t.Fatal(err)
		}
		defer func() {
			sess.LastPasskeyAt = saved
			_ = f.l.deps.Sessions.Update(context.Background(), sess)
		}()
		var tr struct {
			IDToken string `json:"id_token"`
		}
		body(t, f.exchange(f.code(t), verifier).Body, &tr)
		tok, err := jwt.ParseSigned(tr.IDToken, []jose.SignatureAlgorithm{jose.RS256})
		if err != nil {
			t.Fatal(err)
		}
		var ext struct {
			AMR []string `json:"amr"`
		}
		if err := tok.UnsafeClaimsWithoutVerification(&ext); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(ext.AMR, "hwk") {
			t.Fatalf("amr = %v for a session without a passkey", ext.AMR)
		}
	})

	t.Run("CodeSingleUse", func(t *testing.T) {
		code := f.code(t)
		if w := f.exchange(code, verifier); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if w := f.exchange(code, verifier); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_grant") {
			t.Fatalf("reuse = %d", w.Code)
		}
	})

	t.Run("WrongVerifier", func(t *testing.T) {
		if w := f.exchange(f.code(t), strings.Repeat("x", 43)); w.Code != 400 {
			t.Fatalf("got %d", w.Code)
		}
	})

	t.Run("TokenWrongSecret401", func(t *testing.T) {
		w := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {"x"}, "client_id": {f.clientID}, "client_secret": {"wrong"}}, false)
		if w.Code != 401 {
			t.Fatalf("got %d", w.Code)
		}
	})

	t.Run("BasicAuth", func(t *testing.T) {
		w := f.token(url.Values{"grant_type": {"authorization_code"}, "code": {f.code(t)}, "redirect_uri": {nimbusCB}, "code_verifier": {verifier}}, true)
		if w.Code != http.StatusOK {
			t.Fatalf("basic: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("RefreshRotatesAndDiesWithSession", func(t *testing.T) {
		if refresh == "" {
			t.Fatal("no refresh token issued for offline_access")
		}
		form := func(rt string) url.Values {
			return url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}, "client_id": {f.clientID}, "client_secret": {f.secret}}
		}
		w := f.token(form(refresh), false)
		if w.Code != 200 {
			t.Fatalf("refresh = %d %s", w.Code, w.Body.String())
		}
		var tr struct {
			RefreshToken string `json:"refresh_token"`
		}
		body(t, w.Body, &tr)
		if w := f.token(form(refresh), false); w.Code != 400 {
			t.Fatal("old refresh token still works")
		}
		sid := c.cookies[SessionCookie]
		if err := f.l.deps.Sessions.Revoke(context.Background(), sid); err != nil {
			t.Fatal(err)
		}
		if w := f.token(form(tr.RefreshToken), false); w.Code != 400 {
			t.Fatalf("refresh after logout = %d", w.Code)
		}
	})
}
