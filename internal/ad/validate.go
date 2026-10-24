package ad

import (
	"crypto/x509"
	"errors"
	"net/url"
	"strings"
)

// Validate checks an admin-supplied configuration before it is stored. It
// does not contact the directory (see Test) and ignores the sealed password.
func (c Config) Validate() error {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "ldap" && u.Scheme != "ldaps") || u.Host == "" {
		return errors.New("url must start with ldaps:// or ldap:// and include a server")
	}
	if !isLDAPS(c.URL) && !c.StartTLS && !c.AllowPlaintext {
		return errors.New("ldap:// without StartTLS is unencrypted; use ldaps://, turn on StartTLS or explicitly allow plaintext")
	}
	if strings.TrimSpace(c.BindDN) == "" {
		return errors.New("bind_dn is required")
	}
	if strings.TrimSpace(c.BaseDN) == "" {
		return errors.New("base_dn is required")
	}
	if c.UserFilter != "" && !strings.Contains(c.UserFilter, "%s") {
		return errors.New("user_filter must contain %s where the username goes")
	}
	if c.MinTLS != "" && c.MinTLS != "1.2" && c.MinTLS != "1.3" {
		return errors.New("min_tls must be 1.2 or 1.3")
	}
	if c.CAPEM != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(c.CAPEM)) {
		return errors.New("ca_pem does not contain a valid PEM certificate")
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 60 {
		return errors.New("timeout_seconds must be between 0 and 60")
	}
	return nil
}
