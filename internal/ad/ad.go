// Package ad authenticates and looks up users in Active Directory over LDAP.
package ad

import (
	"context"
	"errors"
)

// Config describes the AD connection. Zero values get defaults via withDefaults.
type Config struct {
	URL                string `json:"url"`
	StartTLS           bool   `json:"start_tls"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	CAPEM              string `json:"ca_pem"`
	BindDN             string `json:"bind_dn"`
	BindPasswordSealed string `json:"bind_password_sealed"` // AES-GCM sealed with data_key
	BaseDN             string `json:"base_dn"`
	UserFilter         string `json:"user_filter"`
	MinTLS             string `json:"min_tls"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	AllowPlaintext     bool   `json:"allow_plaintext"` // permit ldap:// without StartTLS
}

const (
	DefaultUserFilter = "(&(objectClass=user)(sAMAccountName=%s))"
	DefaultMinTLS     = "1.2"
	DefaultTimeout    = 5
)

func (c Config) withDefaults() Config {
	if c.UserFilter == "" {
		c.UserFilter = DefaultUserFilter
	}
	if c.MinTLS == "" {
		c.MinTLS = DefaultMinTLS
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = DefaultTimeout
	}
	return c
}

// User is a directory account.
type User struct {
	Username, DisplayName, GivenName, Email, DN string
}

var (
	ErrInvalidCredentials = errors.New("ad: invalid credentials")
	ErrUnavailable        = errors.New("ad: directory unavailable")
	ErrNotFound           = errors.New("ad: user not found")
)

// Directory is the user directory used by the auth flow.
type Directory interface {
	Authenticate(ctx context.Context, username, password string) (User, error)
	Lookup(ctx context.Context, username string) (User, error)
}
