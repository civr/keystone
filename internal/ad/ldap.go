package ad

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/civr/keystone/internal/crypto"
	"github.com/go-ldap/ldap/v3"
)

type ldapDir struct {
	cfg     Config
	dataKey []byte
}

// NewLDAP returns a Directory doing service bind, search, then user bind.
// Every operation is bounded by cfg.TimeoutSeconds.
func NewLDAP(cfg Config, dataKey []byte) Directory {
	return &ldapDir{cfg: cfg.withDefaults(), dataKey: dataKey}
}

func buildFilter(tmpl, username string) string {
	return strings.ReplaceAll(tmpl, "%s", ldap.EscapeFilter(username))
}

func isLDAPS(u string) bool { return strings.HasPrefix(strings.ToLower(u), "ldaps://") }

func tlsConfig(cfg Config) (*tls.Config, error) {
	tc := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // admin-controlled setting
	switch cfg.MinTLS {
	case "", "1.2":
		tc.MinVersion = tls.VersionTLS12
	case "1.3":
		tc.MinVersion = tls.VersionTLS13
	default:
		return nil, fmt.Errorf("ad: unsupported min TLS version %q (use 1.2 or 1.3)", cfg.MinTLS)
	}
	// go-ldap's StartTLS never sets ServerName, so derive it from the URL.
	if u, err := url.Parse(cfg.URL); err == nil {
		tc.ServerName = u.Hostname()
	}
	if cfg.CAPEM != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(cfg.CAPEM)) {
			return nil, errors.New("ad: invalid CA certificate PEM")
		}
		tc.RootCAs = pool
	}
	return tc, nil
}

// connect dials, applies timeouts and closes the connection when ctx ends.
// The returned func must be called to release resources.
func connect(ctx context.Context, cfg Config) (*ldap.Conn, func(), error) {
	if !isLDAPS(cfg.URL) && !cfg.StartTLS && !cfg.AllowPlaintext {
		return nil, nil, errors.New("ad: plaintext ldap:// without StartTLS is not allowed (use ldaps://, enable StartTLS, or allow plaintext explicitly)")
	}
	tc, err := tlsConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	opts := []ldap.DialOpt{ldap.DialWithDialer(&net.Dialer{Timeout: timeout})}
	if isLDAPS(cfg.URL) {
		opts = append(opts, ldap.DialWithTLSConfig(tc))
	}
	conn, err := ldap.DialURL(cfg.URL, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	conn.SetTimeout(timeout)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	closeFn := func() { close(done); _ = conn.Close() }
	if cfg.StartTLS && !isLDAPS(cfg.URL) {
		if err := conn.StartTLS(tc); err != nil {
			closeFn()
			return nil, nil, fmt.Errorf("%w: starttls: %v", ErrUnavailable, err)
		}
	}
	return conn, closeFn, nil
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
		return ErrInvalidCredentials
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func (d *ldapDir) serviceBind(conn *ldap.Conn) error {
	pw, err := crypto.Open(d.dataKey, d.cfg.BindPasswordSealed)
	if err != nil {
		return fmt.Errorf("ad: open bind password: %w", err)
	}
	if err := conn.Bind(d.cfg.BindDN, string(pw)); err != nil {
		// A bad service account is a configuration fault, not a user error.
		return fmt.Errorf("%w: service bind: %v", ErrUnavailable, err)
	}
	return nil
}

func (d *ldapDir) search(conn *ldap.Conn, username string) (User, error) {
	n := NormalizeUsername(username)
	if n == "" {
		return User{}, ErrNotFound
	}
	req := ldap.NewSearchRequest(d.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2,
		d.cfg.TimeoutSeconds, false, buildFilter(d.cfg.UserFilter, n),
		[]string{"sAMAccountName", "displayName", "givenName", "mail"}, nil)
	res, err := conn.Search(req)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return User{}, ErrNotFound // ambiguous match
		}
		return User{}, mapErr(err)
	}
	if len(res.Entries) != 1 {
		return User{}, ErrNotFound
	}
	e := res.Entries[0]
	return User{
		Username: n, DisplayName: e.GetAttributeValue("displayName"), GivenName: e.GetAttributeValue("givenName"),
		Email: e.GetAttributeValue("mail"), DN: e.DN,
	}, nil
}

func (d *ldapDir) withConn(ctx context.Context, fn func(*ldap.Conn) (User, error)) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	conn, closeFn, err := connect(ctx, d.cfg)
	if err != nil {
		return User{}, err
	}
	defer closeFn()
	if err := d.serviceBind(conn); err != nil {
		return User{}, err
	}
	return fn(conn)
}

func (d *ldapDir) Lookup(ctx context.Context, username string) (User, error) {
	return d.withConn(ctx, func(c *ldap.Conn) (User, error) { return d.search(c, username) })
}

func (d *ldapDir) Authenticate(ctx context.Context, username, password string) (User, error) {
	if password == "" { // an empty password would be an unauthenticated bind
		return User{}, ErrInvalidCredentials
	}
	return d.withConn(ctx, func(c *ldap.Conn) (User, error) {
		u, err := d.search(c, username)
		if err != nil {
			return User{}, err
		}
		if err := c.Bind(u.DN, password); err != nil {
			return User{}, mapErr(err)
		}
		return u, nil
	})
}

// Test checks connectivity and the service bind, and counts matching users.
// The filter's %s is replaced by a wildcard (admin-supplied config, not user input).
func Test(ctx context.Context, cfg Config, dataKey []byte) (int, error) {
	d := &ldapDir{cfg: cfg.withDefaults(), dataKey: dataKey}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	conn, closeFn, err := connect(ctx, d.cfg)
	if err != nil {
		return 0, err
	}
	defer closeFn()
	if err := d.serviceBind(conn); err != nil {
		return 0, err
	}
	req := ldap.NewSearchRequest(d.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1000,
		d.cfg.TimeoutSeconds, false, strings.ReplaceAll(d.cfg.UserFilter, "%s", "*"), []string{"dn"}, nil)
	res, err := conn.Search(req)
	if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return 0, mapErr(err)
	}
	if res == nil {
		return 1000, nil
	}
	return len(res.Entries), nil
}
