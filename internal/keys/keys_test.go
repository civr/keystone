package keys

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/testutil"
)

var dataKey = []byte("0123456789abcdef0123456789abcdef")

func newStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	pool, err := db.Open(ctx, testutil.Postgres(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return New(pool, dataKey)
}

func TestKeys(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	t.Run("ActiveCreatesOnce", func(t *testing.T) {
		var wg sync.WaitGroup
		ids := make([]string, 4)
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				k, err := s.Active(ctx, "saml")
				if err != nil {
					t.Error(err)
					return
				}
				ids[i] = k.ID
			}()
		}
		wg.Wait()
		for _, id := range ids[1:] {
			if id != ids[0] || id == "" {
				t.Fatalf("ids differ: %v", ids)
			}
		}
		k, _ := s.Active(ctx, "saml")
		if k.Signer == nil || k.Cert == nil || !k.Signer.PublicKey.Equal(k.Cert.PublicKey) {
			t.Fatal("cert does not match key")
		}
	})

	t.Run("KeyIsSealed", func(t *testing.T) {
		var raw string
		if err := s.pool.QueryRow(ctx, `select key_sealed from signing_keys where use = 'saml'`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "PRIVATE KEY") {
			t.Fatal("key stored in clear")
		}
	})

	t.Run("RotateKeepsPreviousPublished", func(t *testing.T) {
		old, err := s.Active(ctx, "oidc")
		if err != nil {
			t.Fatal(err)
		}
		nk, err := s.Rotate(ctx, "oidc")
		if err != nil {
			t.Fatal(err)
		}
		if nk.ID == old.ID {
			t.Fatal("rotate returned the old key")
		}
		pub, err := s.Published(ctx, "oidc")
		if err != nil {
			t.Fatal(err)
		}
		if len(pub) != 2 || pub[0].ID != nk.ID || pub[1].ID != old.ID {
			t.Fatalf("published = %v", ids(pub))
		}
		if _, err := s.pool.Exec(ctx, `update signing_keys set retired_at = now() - interval '25 hours' where id = $1`, old.ID); err != nil {
			t.Fatal(err)
		}
		if pub, _ = s.Published(ctx, "oidc"); len(pub) != 1 {
			t.Fatalf("published after 25h = %v", ids(pub))
		}
	})

	t.Run("SAMLPublishesLastRetired", func(t *testing.T) {
		a, _ := s.Active(ctx, "saml")
		b, _ := s.Rotate(ctx, "saml")
		c, _ := s.Rotate(ctx, "saml")
		pub, _ := s.Published(ctx, "saml")
		if len(pub) != 2 || pub[0].ID != c.ID || pub[1].ID != b.ID {
			t.Fatalf("published = %v (a=%s)", ids(pub), a.ID)
		}
	})
}

func ids(ks []Key) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = k.ID
	}
	return out
}
