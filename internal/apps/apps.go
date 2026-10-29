// Package apps holds app tiles with their sign-in protocol and the account
// Keystone signs in as (spec §2: "accountkoppeling").
package apps

import (
	"context"
	"errors"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/accounts"
)

// Protocols an app can use.
const (
	Link  = "link"
	SAMLP = "saml"
	OIDCP = "oidc"
	LDAPP = "ldap"
	GateP = "gate"
	Vault = "vault"
)

// Protocols lists every valid protocol.
var Protocols = []string{Link, SAMLP, OIDCP, LDAPP, GateP, Vault}

// Account is the identity sent to the app. Empty fields fall back to the owner.
type Account struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// SAML configures a SAML service provider.
type SAML struct {
	EntityID     string `json:"entity_id"`
	ACSURL       string `json:"acs_url"`
	IdPInitiated bool   `json:"idp_initiated"`
}

// OIDC configures an OpenID Connect client.
type OIDC struct {
	ClientID     string   `json:"client_id"`
	SecretHash   string   `json:"secret_hash,omitempty"`
	RedirectURIs []string `json:"redirect_uris"`
}

// LDAP configures an app that binds to keystone-ldap.
type LDAP struct {
	Slug           string `json:"slug"`
	BindSecretHash string `json:"bind_secret_hash,omitempty"`
}

// Gate configures an app behind keystone-gate.
type Gate struct {
	Host        string      `json:"host"`
	BypassPaths []string    `json:"bypass_paths"`
	SignIn      *GateSignIn `json:"sign_in,omitempty"`
}

// Gate sign-in modes: what keystone-gate does with the credentials stored
// in the vault. basic and header add a request header to every request the
// proxy passes on; form, json and jellyfin log the browser in at the app.
const (
	SignInBasic    = "basic"
	SignInHeader   = "header"
	SignInForm     = "form"
	SignInJSON     = "json"
	SignInJellyfin = "jellyfin"
)

// GateSignIn says how keystone-gate signs the person in to a gate app.
type GateSignIn struct {
	Mode string `json:"mode"`
	// Upstream is the address keystone-gate itself calls (form, json,
	// jellyfin), e.g. http://jellyfin:8096.
	Upstream  string `json:"upstream,omitempty"`
	LoginPath string `json:"login_path,omitempty"`
	UserField string `json:"user_field,omitempty"`
	PassField string `json:"pass_field,omitempty"`
	// Header carries the stored password in header mode.
	Header string `json:"header,omitempty"`
}

// Server reports whether the sign-in is a login keystone-gate performs at
// the app, rather than a header added to proxied requests.
func (g *GateSignIn) Server() bool {
	return g != nil && (g.Mode == SignInForm || g.Mode == SignInJSON || g.Mode == SignInJellyfin)
}

// Vault sign-in modes: copy the password, or post the app's login form
// (form fields, or a JSON body sent as text/plain) with the stored details.
const (
	VaultCopy = "copy"
	VaultForm = "form"
	VaultJSON = "json"
)

// VaultLogin says how a vault tile signs in.
type VaultLogin struct {
	Mode      string `json:"mode"`
	LoginURL  string `json:"login_url,omitempty"`
	UserField string `json:"user_field,omitempty"`
	PassField string `json:"pass_field,omitempty"`
}

// Config is the protocol-specific part of an app (apps.config).
type Config struct {
	Account Account     `json:"account"`
	SAML    *SAML       `json:"saml,omitempty"`
	OIDC    *OIDC       `json:"oidc,omitempty"`
	LDAP    *LDAP       `json:"ldap,omitempty"`
	Gate    *Gate       `json:"gate,omitempty"`
	Vault   *VaultLogin `json:"vault,omitempty"`
	// RequireMFA: only sessions signed in with a passkey or an
	// authenticator code may use the app.
	RequireMFA bool `json:"require_mfa,omitempty"`
}

// App is one tile.
type App struct {
	ID       string
	Name     string
	URL      string
	Protocol string
	Config   Config
	// Access lists who may use the app: account ids and "group:<id>"
	// entries; nil is everyone.
	Access []string
}

// Allowed reports whether account id, in groups, may use a.
func (a App) Allowed(id string, groups ...string) bool {
	if a.Access == nil {
		return true
	}
	if id == "" {
		id = accounts.OwnerID
	}
	if slices.Contains(a.Access, id) {
		return true
	}
	for _, g := range groups {
		if slices.Contains(a.Access, "group:"+g) {
			return true
		}
	}
	return false
}

// AllowedFor reports whether acct may use a.
func (a App) AllowedFor(acct Owner) bool { return a.Allowed(acct.ID, acct.Groups...) }

// Owner is a Keystone account: the owner or a user (package accounts).
type Owner = accounts.Account

var headerRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

var fieldRe = regexp.MustCompile(`^[A-Za-z0-9_.\[\]-]{1,64}$`)

var (
	slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
)

// localHost reports whether host is on the home network, where plain http
// to an app is acceptable.
func localHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	h := strings.ToLower(host)
	for _, s := range []string{".lan", ".local", ".home.arpa", ".internal", ".localhost"} {
		if strings.HasSuffix(h, s) || h == strings.TrimPrefix(s, ".") {
			return true
		}
	}
	return false
}

// appURL checks an absolute http(s) URL without fragment; http only for local hosts.
func appURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Host == "" || u.Fragment != "" || u.User != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return localHost(u.Hostname())
	}
	return false
}

// Validate checks c for protocol and drops the sections of other protocols.
// It returns the offending field and an English message, or "" when valid.
func (c *Config) Validate(protocol string) (field, msg string) {
	for _, f := range []struct{ name, v string }{
		{"username", c.Account.Username}, {"email", c.Account.Email}, {"display_name", c.Account.DisplayName},
	} {
		if utf8.RuneCountInString(f.v) > 256 || strings.ContainsAny(f.v, "\x00\r\n") {
			return "config.account." + f.name, "At most 256 characters, no line breaks."
		}
	}
	saml, oidc, ldap, gate, vault := c.SAML, c.OIDC, c.LDAP, c.Gate, c.Vault
	c.SAML, c.OIDC, c.LDAP, c.Gate, c.Vault = nil, nil, nil, nil, nil
	switch protocol {
	case Link:
	case Vault:
		if vault == nil || vault.Mode == "" || vault.Mode == VaultCopy {
			break
		}
		if vault.Mode != VaultForm && vault.Mode != VaultJSON {
			return "config.vault.mode", "Choose how the tile signs in."
		}
		if !appURL(vault.LoginURL) {
			return "config.vault.login_url", "Enter the address the login form is sent to; http is only allowed on your home network."
		}
		if vault.UserField == "" {
			vault.UserField = "username"
		}
		if vault.PassField == "" {
			vault.PassField = "password"
		}
		if !fieldRe.MatchString(vault.UserField) || !fieldRe.MatchString(vault.PassField) {
			return "config.vault.user_field", "Field names use letters, digits and . _ - [ ] (at most 64)."
		}
		c.Vault = vault
	case SAMLP:
		if saml == nil {
			return "config.saml", "Enter the SAML details of the app."
		}
		saml.EntityID = strings.TrimSpace(saml.EntityID)
		if saml.EntityID == "" || len(saml.EntityID) > 1024 {
			return "config.saml.entity_id", "Enter the entity ID of the app."
		}
		if !appURL(saml.ACSURL) {
			return "config.saml.acs_url", "The ACS URL must be absolute; http is only allowed for addresses on your home network."
		}
		c.SAML = saml
	case OIDCP:
		if oidc == nil || len(oidc.RedirectURIs) == 0 || len(oidc.RedirectURIs) > 10 {
			return "config.oidc.redirect_uris", "Enter 1 to 10 redirect URIs."
		}
		for _, u := range oidc.RedirectURIs {
			if !appURL(u) {
				return "config.oidc.redirect_uris", "Every redirect URI must be an absolute URL without a # part; http is only allowed on your home network."
			}
		}
		c.OIDC = oidc
	case LDAPP:
		if c.RequireMFA {
			return "config.require_mfa", "LDAP apps check only a password; a second factor cannot be required."
		}
		if ldap == nil || !slugRe.MatchString(ldap.Slug) {
			return "config.ldap.slug", "Use 1 to 32 lowercase letters, digits or dashes."
		}
		c.LDAP = ldap
	case GateP:
		if gate == nil || !hostRe.MatchString(gate.Host) {
			return "config.gate.host", "Enter the hostname of the app, such as ha.home.lan."
		}
		if gate.BypassPaths == nil {
			gate.BypassPaths = []string{}
		}
		if len(gate.BypassPaths) > 32 {
			return "config.gate.bypass_paths", "Maximaal 32 uitzonderingen."
		}
		for _, p := range gate.BypassPaths {
			if !strings.HasPrefix(p, "/") || len(p) > 256 || strings.ContainsAny(p, " ?#") {
				return "config.gate.bypass_paths", "Every exception is a path that starts with /."
			}
		}
		if msg := gate.validateSignIn(); msg != nil {
			return msg[0], msg[1]
		}
		c.Gate = gate
	default:
		return "protocol", "Onbekend protocol."
	}
	return "", ""
}

// validateSignIn checks and completes g.SignIn; it returns the offending
// field and message, or nil.
func (g *Gate) validateSignIn() []string {
	si := g.SignIn
	if si == nil || si.Mode == "" {
		g.SignIn = nil
		return nil
	}
	bad := func(f, m string) []string { return []string{"config.gate.sign_in." + f, m} }
	keep := *si
	switch si.Mode {
	case SignInBasic:
		keep = GateSignIn{Mode: si.Mode}
	case SignInHeader:
		h := strings.TrimSpace(si.Header)
		l := strings.ToLower(h)
		if !headerRe.MatchString(h) || l == "host" || l == "cookie" || l == "content-length" || l == "transfer-encoding" || l == "connection" ||
			strings.HasPrefix(l, "x-forwarded-") || strings.HasPrefix(l, "x-keystone-") {
			return bad("header", "Enter a header name such as X-Api-Key; it cannot be a header the proxy sets itself.")
		}
		keep = GateSignIn{Mode: si.Mode, Header: h}
	case SignInForm, SignInJSON, SignInJellyfin:
		u, err := url.Parse(si.Upstream)
		if err != nil || len(si.Upstream) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
			u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return bad("upstream", "Enter the address Keystone uses to reach the app, such as http://jellyfin:8096.")
		}
		keep = GateSignIn{Mode: si.Mode, Upstream: strings.TrimRight(si.Upstream, "/")}
		if si.Mode == SignInJellyfin {
			break
		}
		if !strings.HasPrefix(si.LoginPath, "/") || strings.HasPrefix(si.LoginPath, "//") || len(si.LoginPath) > 256 || strings.ContainsAny(si.LoginPath, " #\\\x00\r\n") {
			return bad("login_path", "Enter the path of the login, starting with /.")
		}
		if si.UserField == "" {
			si.UserField = "username"
		}
		if si.PassField == "" {
			si.PassField = "password"
		}
		if !fieldRe.MatchString(si.UserField) || !fieldRe.MatchString(si.PassField) {
			return bad("user_field", "Field names use letters, digits and . _ - [ ] (at most 64).")
		}
		keep.LoginPath, keep.UserField, keep.PassField = si.LoginPath, si.UserField, si.PassField
	default:
		return bad("mode", "Choose how Keystone signs you in.")
	}
	g.SignIn = &keep
	return nil
}

// Mapped returns the account to send, filling empty fields from the owner.
func (a App) Mapped(o Owner) Account {
	// The per-app overrides name the owner's account at the app; everyone
	// else is known there by their own name.
	m := a.Config.Account
	if !o.IsOwner() {
		m = Account{}
	}
	if m.Username == "" {
		m.Username = o.Username
	}
	if m.Email == "" {
		m.Email = o.Email
	}
	if m.DisplayName == "" {
		m.DisplayName = o.DisplayName
	}
	return m
}

// Querier is satisfied by *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const cols = `id::text, name, url, protocol, config, access`

func scan(r pgx.CollectableRow) (App, error) {
	var a App
	return a, r.Scan(&a.ID, &a.Name, &a.URL, &a.Protocol, &a.Config, &a.Access)
}

// Load returns every app in tile order.
func Load(ctx context.Context, q Querier) ([]App, error) {
	rows, err := q.Query(ctx, `select `+cols+` from apps order by sort, created_at, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scan)
}

// ErrNotFound means no app has that id.
var ErrNotFound = errors.New("apps: not found")

// ByID loads one app.
func ByID(ctx context.Context, q Querier, id string) (App, error) {
	rows, err := q.Query(ctx, `select `+cols+` from apps where id::text = $1`, id)
	if err != nil {
		return App{}, err
	}
	a, err := pgx.CollectOneRow(rows, scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return App{}, ErrNotFound
	}
	return a, err
}

// LoadOwner returns the owner chosen during setup.
func LoadOwner(ctx context.Context, q Querier) (Owner, error) {
	o, err := accounts.Owner(ctx, q)
	if errors.Is(err, accounts.ErrNotFound) {
		err = pgx.ErrNoRows
	}
	return o, err
}
