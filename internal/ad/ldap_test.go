package ad

import (
	"context"
	"errors"
	"testing"

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
