// Package apps holds app tiles with their sign-in protocol and the account
// Keystone signs in as (spec §2: "accountkoppeling").
package apps

import (
	"context"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
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
	Host        string   `json:"host"`
	BypassPaths []string `json:"bypass_paths"`
}

// Config is the protocol-specific part of an app (apps.config).
type Config struct {
	Account Account `json:"account"`
	SAML    *SAML   `json:"saml,omitempty"`
	OIDC    *OIDC   `json:"oidc,omitempty"`
	LDAP    *LDAP   `json:"ldap,omitempty"`
	Gate    *Gate   `json:"gate,omitempty"`
}

// App is one tile.
type App struct {
	ID       string
	Name     string
	URL      string
	Protocol string
	Config   Config
}

// Owner is the Keystone user.
type Owner struct {
	Username    string
	DisplayName string
	Email       string
	UserHandle  []byte
	// PasswordHash is the Keystone password of an owner made in the short
	// setup (argon2); empty when the owner signs in with AD.
	PasswordHash string
}

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
// It returns the offending field and a Dutch message, or "" when valid.
func (c *Config) Validate(protocol string) (field, msg string) {
	for _, f := range []struct{ name, v string }{
		{"username", c.Account.Username}, {"email", c.Account.Email}, {"display_name", c.Account.DisplayName},
	} {
		if utf8.RuneCountInString(f.v) > 256 || strings.ContainsAny(f.v, "\x00\r\n") {
			return "config.account." + f.name, "Maximaal 256 tekens, zonder regeleinden."
		}
	}
	saml, oidc, ldap, gate := c.SAML, c.OIDC, c.LDAP, c.Gate
	c.SAML, c.OIDC, c.LDAP, c.Gate = nil, nil, nil, nil
	switch protocol {
	case Link, Vault:
	case SAMLP:
		if saml == nil {
			return "config.saml", "Vul de SAML-gegevens van de app in."
		}
		saml.EntityID = strings.TrimSpace(saml.EntityID)
		if saml.EntityID == "" || len(saml.EntityID) > 1024 {
			return "config.saml.entity_id", "Vul de entity ID van de app in."
		}
		if !appURL(saml.ACSURL) {
			return "config.saml.acs_url", "De ACS-URL moet absoluut zijn; http alleen voor adressen in je thuisnetwerk."
		}
		c.SAML = saml
	case OIDCP:
		if oidc == nil || len(oidc.RedirectURIs) == 0 || len(oidc.RedirectURIs) > 10 {
			return "config.oidc.redirect_uris", "Vul 1 tot 10 redirect-URI's in."
		}
		for _, u := range oidc.RedirectURIs {
			if !appURL(u) {
				return "config.oidc.redirect_uris", "Elke redirect-URI moet een absolute URL zonder #-deel zijn; http alleen in je thuisnetwerk."
			}
		}
		c.OIDC = oidc
	case LDAPP:
		if ldap == nil || !slugRe.MatchString(ldap.Slug) {
			return "config.ldap.slug", "Gebruik 1 tot 32 kleine letters, cijfers of streepjes."
		}
		c.LDAP = ldap
	case GateP:
		if gate == nil || !hostRe.MatchString(gate.Host) {
			return "config.gate.host", "Vul de hostnaam van de app in, zoals ha.home.lan."
		}
		if gate.BypassPaths == nil {
			gate.BypassPaths = []string{}
		}
		if len(gate.BypassPaths) > 32 {
			return "config.gate.bypass_paths", "Maximaal 32 uitzonderingen."
		}
		for _, p := range gate.BypassPaths {
			if !strings.HasPrefix(p, "/") || len(p) > 256 || strings.ContainsAny(p, " ?#") {
				return "config.gate.bypass_paths", "Elke uitzondering is een pad dat met / begint."
			}
		}
		c.Gate = gate
	default:
		return "protocol", "Onbekend protocol."
	}
	return "", ""
}

// Mapped returns the account to send, filling empty fields from the owner.
func (a App) Mapped(o Owner) Account {
	m := a.Config.Account
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

const cols = `id::text, name, url, protocol, config`

func scan(r pgx.CollectableRow) (App, error) {
	var a App
	return a, r.Scan(&a.ID, &a.Name, &a.URL, &a.Protocol, &a.Config)
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
	var o Owner
	err := q.QueryRow(ctx, `select username, coalesce(display_name, ''), coalesce(email, ''), user_handle,
		coalesce(password_hash, '') from owner where id = 1`).
		Scan(&o.Username, &o.DisplayName, &o.Email, &o.UserHandle, &o.PasswordHash)
	return o, err
}
