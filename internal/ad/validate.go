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
		return errors.New("url moet beginnen met ldaps:// of ldap:// en een server bevatten")
	}
	if !isLDAPS(c.URL) && !c.StartTLS && !c.AllowPlaintext {
		return errors.New("ldap:// zonder StartTLS is onversleuteld; gebruik ldaps://, zet StartTLS aan of sta onversleuteld expliciet toe")
	}
	if strings.TrimSpace(c.BindDN) == "" {
		return errors.New("bind_dn is verplicht")
	}
	if strings.TrimSpace(c.BaseDN) == "" {
		return errors.New("base_dn is verplicht")
	}
	if c.UserFilter != "" && !strings.Contains(c.UserFilter, "%s") {
		return errors.New("user_filter moet %s bevatten op de plek van de gebruikersnaam")
	}
	if c.MinTLS != "" && c.MinTLS != "1.2" && c.MinTLS != "1.3" {
		return errors.New("min_tls moet 1.2 of 1.3 zijn")
	}
	if c.CAPEM != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(c.CAPEM)) {
		return errors.New("ca_pem bevat geen geldig PEM-certificaat")
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 60 {
		return errors.New("timeout_seconds moet tussen 0 en 60 liggen")
	}
	return nil
}
