//go:build integration

package ad

import (
	"context"
	"errors"
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
		BindPasswordSealed: sealed, BaseDN: "ou=people,dc=example,dc=org", UserFilter: testFilter,
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
