//go:build integration

package ad

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/civr/keystone/internal/crypto"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const testFilter = "(&(objectClass=inetOrgPerson)(uid=%s))"

func startLDAP(t *testing.T) Config {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "osixia/openldap:1.5.0",
			ExposedPorts: []string{"389/tcp"},
			Env: map[string]string{
				"LDAP_ORGANISATION":   "Example",
				"LDAP_DOMAIN":         "example.org",
				"LDAP_ADMIN_PASSWORD": "admin",
			},
			Files: []testcontainers.ContainerFile{{
				HostFilePath: "testdata/seed.ldif", ContainerFilePath: "/seed.ldif", FileMode: 0o644,
			}},
			WaitingFor: wait.ForLog("slapd starting").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start openldap: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	code, out, err := c.Exec(ctx, []string{"ldapadd", "-x", "-D", "cn=admin,dc=example,dc=org", "-w", "admin", "-f", "/seed.ldif"})
	if err != nil || code != 0 {
		b := make([]byte, 2048)
		if out != nil {
			n, _ := out.Read(b)
			b = b[:n]
		}
		t.Fatalf("ldapadd code=%d err=%v out=%s", code, err, b)
	}
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "389/tcp")
	key := make([]byte, 32)
	sealed, err := crypto.Seal(key, []byte("admin"))
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		URL: "ldap://" + host + ":" + port.Port(), BindDN: "cn=admin,dc=example,dc=org",
		AllowPlaintext: true, BindPasswordSealed: sealed, BaseDN: "ou=people,dc=example,dc=org", UserFilter: testFilter,
	}
}

func TestAuthenticateOK(t *testing.T) {
	cfg := startLDAP(t)
	d := NewLDAP(cfg, make([]byte, 32))
	u, err := d.Authenticate(context.Background(), `HOME\Thijs`, "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "thijs" || u.GivenName != "Thijs" || u.DisplayName != "Thijs Willems" ||
		u.Email != "thijs@example.org" || u.DN != "uid=thijs,ou=people,dc=example,dc=org" {
		t.Fatalf("unexpected user %+v", u)
	}
	n, err := Test(context.Background(), cfg, make([]byte, 32))
	if err != nil || n != 1 {
		t.Fatalf("Test = %d, %v", n, err)
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	cfg := startLDAP(t)
	d := NewLDAP(cfg, make([]byte, 32))
	if _, err := d.Authenticate(context.Background(), "thijs", "nope"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
	if _, err := d.Authenticate(context.Background(), "ghost", "nope"); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: %v", err)
	}
	if _, err := d.Lookup(context.Background(), "*)(uid=*"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("injection should find nothing, got %v", err)
	}
}

func TestUnreachableReturnsWithinTimeout(t *testing.T) {
	d := NewLDAP(Config{URL: "ldaps://10.255.255.1:636", TimeoutSeconds: 1}, make([]byte, 32))
	start := time.Now()
	_, err := d.Authenticate(context.Background(), "thijs", "pw")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if el := time.Since(start); el > 1500*time.Millisecond {
		t.Fatalf("took %v", el)
	}
}

// testPKI returns a CA PEM plus a server cert/key PEM valid for localhost and 127.0.0.1.
func testPKI(t *testing.T) (caPEM, certPEM, keyPEM []byte) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	srvKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(srvKey)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func TestStartTLSAndLDAPSWithVerification(t *testing.T) {
	ca, crt, key := testPKI(t)
	dir := t.TempDir()
	for name, b := range map[string][]byte{"ca.crt": ca, "ldap.crt": crt, "ldap.key": key} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	certs := "/container/service/slapd/assets/certs/"
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "osixia/openldap:1.5.0",
			ExposedPorts: []string{"389/tcp", "636/tcp"},
			Env: map[string]string{
				"LDAP_ORGANISATION": "Example", "LDAP_DOMAIN": "example.org", "LDAP_ADMIN_PASSWORD": "admin",
				"LDAP_TLS_CRT_FILENAME": "ldap.crt", "LDAP_TLS_KEY_FILENAME": "ldap.key",
				"LDAP_TLS_CA_CRT_FILENAME": "ca.crt", "LDAP_TLS_VERIFY_CLIENT": "never",
			},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: filepath.Join(dir, "ca.crt"), ContainerFilePath: certs + "ca.crt", FileMode: 0o644},
				{HostFilePath: filepath.Join(dir, "ldap.crt"), ContainerFilePath: certs + "ldap.crt", FileMode: 0o644},
				{HostFilePath: filepath.Join(dir, "ldap.key"), ContainerFilePath: certs + "ldap.key", FileMode: 0o644},
				{HostFilePath: "testdata/seed.ldif", ContainerFilePath: "/seed.ldif", FileMode: 0o644},
			},
			WaitingFor: wait.ForLog("slapd starting").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start openldap: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	if code, _, err := c.Exec(ctx, []string{"ldapadd", "-x", "-D", "cn=admin,dc=example,dc=org", "-w", "admin", "-f", "/seed.ldif"}); err != nil || code != 0 {
		t.Fatalf("ldapadd code=%d err=%v", code, err)
	}
	host, _ := c.Host(ctx)
	p389, _ := c.MappedPort(ctx, "389/tcp")
	p636, _ := c.MappedPort(ctx, "636/tcp")
	key32 := make([]byte, 32)
	sealed, _ := crypto.Seal(key32, []byte("admin"))
	base := Config{
		BindDN: "cn=admin,dc=example,dc=org", BindPasswordSealed: sealed, CAPEM: string(ca),
		BaseDN: "ou=people,dc=example,dc=org", UserFilter: testFilter, TimeoutSeconds: 10,
	}
	st := base
	st.URL, st.StartTLS = "ldap://"+host+":"+p389.Port(), true
	ls := base
	ls.URL = "ldaps://" + host + ":" + p636.Port()
	for name, cfg := range map[string]Config{"starttls": st, "ldaps": ls} {
		u, err := NewLDAP(cfg, key32).Authenticate(ctx, "thijs", "correct-horse-battery")
		if err != nil || u.Username != "thijs" {
			t.Errorf("%s: %+v %v", name, u, err)
		}
	}
	// Verification really is on: without the CA the handshake must fail.
	bad := st
	bad.CAPEM = ""
	if _, err := NewLDAP(bad, key32).Lookup(ctx, "thijs"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected verification failure without CA, got %v", err)
	}
}
