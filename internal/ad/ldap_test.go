package ad

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/civr/keystone/internal/crypto"
)

// Filter injection must be neutralised before any connection is needed to
// matter: with an unreachable server the escaped query never matches, and
// the escaping itself is verified directly.
func TestFilterInjectionEscaped(t *testing.T) {
	got := buildFilter("(&(objectClass=user)(sAMAccountName=%s))", "*)(uid=*")
	want := `(&(objectClass=user)(sAMAccountName=\2a\29\28uid=\2a))`
	if got != want {
		t.Fatalf("buildFilter = %q, want %q", got, want)
	}
}

func TestEmptyPasswordRejected(t *testing.T) {
	key := make([]byte, 32)
	sealed, _ := crypto.Seal(key, []byte("x"))
	d := NewLDAP(Config{URL: "ldap://127.0.0.1:1", BindPasswordSealed: sealed}, key)
	if _, err := d.Authenticate(context.Background(), "thijs", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
}

func TestPlaintextRejectedUnlessAllowed(t *testing.T) {
	key := make([]byte, 32)
	sealed, _ := crypto.Seal(key, []byte("x"))
	cfg := Config{URL: "ldap://127.0.0.1:1", BindPasswordSealed: sealed}
	_, err := NewLDAP(cfg, key).Authenticate(context.Background(), "thijs", "pw")
	if err == nil || !strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("want plaintext error, got %v", err)
	}
	if _, err := Test(context.Background(), cfg, key); err == nil || !strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("Test: want plaintext error, got %v", err)
	}
	cfg.AllowPlaintext = true
	if _, err := NewLDAP(cfg, key).Lookup(context.Background(), "thijs"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("allowed plaintext should reach dial and fail unavailable, got %v", err)
	}
}

func TestTLSConfig(t *testing.T) {
	if _, err := tlsConfig(Config{MinTLS: "1.1"}); err == nil {
		t.Error("MinTLS 1.1 must be rejected")
	}
	if _, err := tlsConfig(Config{MinTLS: "bogus"}); err == nil {
		t.Error("bogus MinTLS must be rejected")
	}
	if _, err := tlsConfig(Config{MinTLS: "1.2", CAPEM: "not a pem"}); err == nil {
		t.Error("bad CA PEM must error")
	}
	tc, err := tlsConfig(Config{MinTLS: "1.3", InsecureSkipVerify: true, URL: "ldaps://dc.home.lan:636"})
	if err != nil || tc.MinVersion != 0x0304 || !tc.InsecureSkipVerify || tc.ServerName != "dc.home.lan" {
		t.Fatalf("got %+v %v", tc, err)
	}
	tc, err = tlsConfig(Config{URL: "ldap://10.0.0.5:389"})
	if err != nil || tc.MinVersion != 0x0303 || tc.InsecureSkipVerify || tc.ServerName != "10.0.0.5" || tc.RootCAs != nil {
		t.Fatalf("got %+v %v", tc, err)
	}
}

func TestSlowADReturnsWithinTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // accept, never answer
		}
	}()
	key := make([]byte, 32)
	sealed, _ := crypto.Seal(key, []byte("x"))
	d := NewLDAP(Config{URL: "ldap://" + ln.Addr().String(), AllowPlaintext: true, BindPasswordSealed: sealed, TimeoutSeconds: 1}, key)
	start := time.Now()
	_, err = d.Authenticate(context.Background(), "thijs", "pw")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if el := time.Since(start); el > 1500*time.Millisecond {
		t.Fatalf("took %v", el)
	}
}
