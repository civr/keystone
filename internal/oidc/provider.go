// Package oidc is Keystone's OpenID Connect provider (spec §2, §9): the
// authorization code flow with mandatory PKCE (S256) for the apps configured
// with protocol oidc. Codes, refresh tokens and access-token handles live in
// Valkey and are bound to the Keystone session, so they die with it.
package oidc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/keys"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

// Endpoint paths under the issuer.
const (
	AuthorizePath = "/oidc/authorize"
	TokenPath     = "/oidc/token"
	UserinfoPath  = "/oidc/userinfo"
	JWKSPath      = "/oidc/jwks"
	DiscoveryPath = "/.well-known/openid-configuration"

	codeTTL = 60 * time.Second
)

// Scopes Keystone understands; others are ignored.
var knownScopes = []string{"openid", "profile", "email", "offline_access"}

// Discovery returns the provider metadata for issuer.
func Discovery(issuer string) map[string]any {
	return map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + AuthorizePath,
		"token_endpoint":                        issuer + TokenPath,
		"userinfo_endpoint":                     issuer + UserinfoPath,
		"jwks_uri":                              issuer + JWKSPath,
		"response_types_supported":              []string{"code"},
		"response_modes_supported":              []string{"query"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"scopes_supported":                      knownScopes,
		"claims_supported":                      []string{"sub", "iss", "aud", "exp", "iat", "auth_time", "nonce", "preferred_username", "email", "name", "amr"},
	}
}

// Error is an OAuth error. Redirect reports whether it may be sent to the
// client's redirect_uri (only once that URI is known to be registered).
type Error struct {
	Code, Description string
	Redirect          bool
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

// AuthRequest is a validated authorization request.
type AuthRequest struct {
	App                                         apps.App
	RedirectURI, State, Nonce, Challenge, Scope string
}

// ParseAuthorize validates an authorization request against the apps.
func ParseAuthorize(q url.Values, list []apps.App) (AuthRequest, *Error) {
	a, ok := clientByID(list, q.Get("client_id"))
	if !ok {
		return AuthRequest{}, &Error{Code: "invalid_client", Description: "Unknown client_id."}
	}
	ru := q.Get("redirect_uri")
	if !slices.Contains(a.Config.OIDC.RedirectURIs, ru) {
		return AuthRequest{}, &Error{Code: "invalid_request", Description: "redirect_uri is not registered for this app."}
	}
	ar := AuthRequest{App: a, RedirectURI: ru, State: q.Get("state"), Nonce: q.Get("nonce")}
	fail := func(code, desc string) (AuthRequest, *Error) {
		return ar, &Error{Code: code, Description: desc, Redirect: true}
	}
	if q.Get("response_type") != "code" {
		return fail("unsupported_response_type", "Only response_type=code is supported.")
	}
	scopes := strings.Fields(q.Get("scope"))
	if !slices.Contains(scopes, "openid") {
		return fail("invalid_scope", "The scope must include openid.")
	}
	scopes = slices.DeleteFunc(scopes, func(s string) bool { return !slices.Contains(knownScopes, s) })
	slices.Sort(scopes)
	ar.Scope = strings.Join(slices.Compact(scopes), " ")
	ar.Challenge = q.Get("code_challenge")
	if q.Get("code_challenge_method") != "S256" || len(ar.Challenge) != 43 {
		return fail("invalid_request", "PKCE with code_challenge_method=S256 is required.")
	}
	if len(ar.State) > 1024 || len(ar.Nonce) > 1024 {
		return fail("invalid_request", "state or nonce is too long.")
	}
	// Sessions are checked before this point, so prompt=none needs nothing
	// extra; request objects are not supported.
	if q.Get("request") != "" || q.Get("request_uri") != "" {
		return fail("request_not_supported", "Request objects are not supported.")
	}
	return ar, nil
}

func clientByID(list []apps.App, id string) (apps.App, bool) {
	for _, a := range list {
		if a.Protocol == apps.OIDCP && a.Config.OIDC != nil && id != "" && a.Config.OIDC.ClientID == id {
			return a, true
		}
	}
	return apps.App{}, false
}

// ClientByID finds the oidc app with client id.
func ClientByID(list []apps.App, id string) (apps.App, bool) { return clientByID(list, id) }

// grant is what a code or refresh token stands for.
type grant struct {
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri,omitempty"`
	Challenge   string `json:"challenge,omitempty"`
	Nonce       string `json:"nonce,omitempty"`
	Scope       string `json:"scope"`
	SessionID   string `json:"session_id"`
}

// Provider issues codes and tokens.
type Provider struct {
	Keys     *keys.Store
	Valkey   *redis.Client
	Sessions *session.Store
	// Account loads the account a session belongs to (session.UserID; "" is
	// the owner).
	Account func(ctx context.Context, userID string) (apps.Owner, error)
	Now     func() time.Time
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func hashKey(prefix, tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return prefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

// IssueCode stores a one-time authorization code for ar in sessionID.
func (p *Provider) IssueCode(ctx context.Context, ar AuthRequest, sessionID string, random func() (string, error)) (string, error) {
	code, err := random()
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(grant{
		ClientID: ar.App.Config.OIDC.ClientID, RedirectURI: ar.RedirectURI, Challenge: ar.Challenge,
		Nonce: ar.Nonce, Scope: ar.Scope, SessionID: sessionID,
	})
	return code, p.Valkey.Set(ctx, hashKey("oidccode:", code), raw, codeTTL).Err()
}

// Tokens is a token endpoint response.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

var errGrant = &Error{Code: "invalid_grant", Description: "The code or token is invalid or has expired."}

// Exchange redeems an authorization code.
func (p *Provider) Exchange(ctx context.Context, cfg settings.Settings, issuer string, client apps.App,
	code, redirectURI, verifier string, random func() (string, error)) (Tokens, error) {
	raw, err := p.Valkey.GetDel(ctx, hashKey("oidccode:", code)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Tokens{}, errGrant
	}
	if err != nil {
		return Tokens{}, err
	}
	var g grant
	if err := json.Unmarshal(raw, &g); err != nil {
		return Tokens{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if g.ClientID != client.Config.OIDC.ClientID || g.RedirectURI != redirectURI ||
		subtle.ConstantTimeCompare([]byte(want), []byte(g.Challenge)) != 1 {
		return Tokens{}, errGrant
	}
	return p.issue(ctx, cfg, issuer, client, g, true, random)
}

// Refresh redeems (and rotates) a refresh token.
func (p *Provider) Refresh(ctx context.Context, cfg settings.Settings, issuer string, client apps.App,
	token string, random func() (string, error)) (Tokens, error) {
	raw, err := p.Valkey.GetDel(ctx, hashKey("oidcrt:", token)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Tokens{}, errGrant
	}
	if err != nil {
		return Tokens{}, err
	}
	var g grant
	if err := json.Unmarshal(raw, &g); err != nil {
		return Tokens{}, err
	}
	if g.ClientID != client.Config.OIDC.ClientID {
		return Tokens{}, errGrant
	}
	g.Nonce = ""
	return p.issue(ctx, cfg, issuer, client, g, false, random)
}

func (p *Provider) issue(ctx context.Context, cfg settings.Settings, issuer string, client apps.App,
	g grant, withID bool, random func() (string, error)) (Tokens, error) {
	now := p.now()
	sess, err := p.Sessions.Peek(ctx, g.SessionID, cfg, now)
	if errors.Is(err, session.ErrNotFound) {
		return Tokens{}, errGrant
	}
	if err != nil {
		return Tokens{}, err
	}
	owner, err := p.account(ctx, sess.UserID, client)
	if err != nil {
		return Tokens{}, err
	}
	if client.Config.RequireMFA && !sess.MFA() {
		return Tokens{}, errGrant
	}
	key, err := p.Keys.Active(ctx, keys.OIDC)
	if err != nil {
		return Tokens{}, err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key.Signer, KeyID: key.ID}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return Tokens{}, err
	}
	acct := client.Mapped(owner)
	sub := base64.RawURLEncoding.EncodeToString(owner.UserHandle)
	jti, err := random()
	if err != nil {
		return Tokens{}, err
	}
	accessTTL := time.Duration(cfg.Tokens.AccessMinutes) * time.Minute
	at, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer: issuer, Subject: sub, Audience: jwt.Audience{client.Config.OIDC.ClientID},
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(accessTTL)), ID: jti,
	}).Claims(map[string]any{"scope": g.Scope, "client_id": client.Config.OIDC.ClientID}).Serialize()
	if err != nil {
		return Tokens{}, err
	}
	raw, _ := json.Marshal(grant{ClientID: client.Config.OIDC.ClientID, Scope: g.Scope, SessionID: g.SessionID})
	if err := p.Valkey.Set(ctx, "oidcat:"+jti, raw, accessTTL).Err(); err != nil {
		return Tokens{}, err
	}
	t := Tokens{AccessToken: at, TokenType: "Bearer", ExpiresIn: int(accessTTL / time.Second), Scope: g.Scope}
	if withID {
		claims := map[string]any{
			"auth_time": sess.LastFullAuthAt.Unix(),
			"amr":       sess.AMR(),
		}
		if g.Nonce != "" {
			claims["nonce"] = g.Nonce
		}
		scopes := strings.Fields(g.Scope)
		if slices.Contains(scopes, "profile") {
			claims["preferred_username"], claims["name"] = acct.Username, acct.DisplayName
		}
		if slices.Contains(scopes, "email") && acct.Email != "" {
			claims["email"], claims["email_verified"] = acct.Email, true
		}
		if t.IDToken, err = jwt.Signed(signer).Claims(jwt.Claims{
			Issuer: issuer, Subject: sub, Audience: jwt.Audience{client.Config.OIDC.ClientID},
			IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Duration(cfg.Tokens.IDMinutes) * time.Minute)),
		}).Claims(claims).Serialize(); err != nil {
			return Tokens{}, err
		}
	}
	if cfg.Tokens.RefreshDays > 0 && slices.Contains(strings.Fields(g.Scope), "offline_access") {
		rt, err := random()
		if err != nil {
			return Tokens{}, err
		}
		raw, _ := json.Marshal(grant{ClientID: client.Config.OIDC.ClientID, Scope: g.Scope, SessionID: g.SessionID})
		if err := p.Valkey.Set(ctx, hashKey("oidcrt:", rt), raw, time.Duration(cfg.Tokens.RefreshDays)*24*time.Hour).Err(); err != nil {
			return Tokens{}, err
		}
		t.RefreshToken = rt
	}
	return t, nil
}

// account loads the session's account and checks it may use client; a gone
// or excluded account is an invalid grant.
func (p *Provider) account(ctx context.Context, userID string, client apps.App) (apps.Owner, error) {
	o, err := p.Account(ctx, userID)
	if errors.Is(err, accounts.ErrNotFound) || (err == nil && (o.Disabled || !client.AllowedFor(o))) {
		return o, errGrant
	}
	return o, err
}

// JWKS returns the published OIDC public keys.
func (p *Provider) JWKS(ctx context.Context) (jose.JSONWebKeySet, error) {
	pub, err := p.Keys.Published(ctx, keys.OIDC)
	if err != nil {
		return jose.JSONWebKeySet{}, err
	}
	set := jose.JSONWebKeySet{}
	for _, k := range pub {
		set.Keys = append(set.Keys, jose.JSONWebKey{Key: &k.Signer.PublicKey, KeyID: k.ID, Algorithm: "RS256", Use: "sig"})
	}
	return set, nil
}

// Userinfo verifies a bearer access token and returns the claims for its app.
func (p *Provider) Userinfo(ctx context.Context, cfg settings.Settings, issuer, bearer string, list []apps.App) (map[string]any, error) {
	tok, err := jwt.ParseSigned(bearer, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, errToken
	}
	set, err := p.JWKS(ctx)
	if err != nil {
		return nil, err
	}
	var c jwt.Claims
	var extra struct {
		Scope    string `json:"scope"`
		ClientID string `json:"client_id"`
	}
	ok := false
	for _, k := range set.Keys {
		if err := tok.Claims(k.Key, &c, &extra); err == nil {
			ok = true
			break
		}
	}
	if !ok || c.ValidateWithLeeway(jwt.Expected{Issuer: issuer, Time: p.now()}, 30*time.Second) != nil {
		return nil, errToken
	}
	raw, err := p.Valkey.Get(ctx, "oidcat:"+c.ID).Bytes()
	if err != nil {
		return nil, errToken
	}
	var g grant
	if json.Unmarshal(raw, &g) != nil || g.ClientID != extra.ClientID {
		return nil, errToken
	}
	sess, err := p.Sessions.Peek(ctx, g.SessionID, cfg, p.now())
	if err != nil {
		return nil, errToken
	}
	client, ok := clientByID(list, g.ClientID)
	if !ok {
		return nil, errToken
	}
	owner, err := p.account(ctx, sess.UserID, client)
	if err == errGrant {
		return nil, errToken
	}
	if err != nil {
		return nil, err
	}
	acct := client.Mapped(owner)
	out := map[string]any{"sub": c.Subject}
	scopes := strings.Fields(g.Scope)
	if slices.Contains(scopes, "profile") {
		out["preferred_username"], out["name"] = acct.Username, acct.DisplayName
	}
	if slices.Contains(scopes, "email") && acct.Email != "" {
		out["email"], out["email_verified"] = acct.Email, true
	}
	return out, nil
}

var errToken = &Error{Code: "invalid_token", Description: "The access token is invalid or has expired."}
