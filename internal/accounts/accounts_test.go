package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/testutil"
)

func TestAccounts(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Open(ctx, testutil.Postgres(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := Owner(ctx, pool); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner before setup: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into owner (id, username, display_name, user_handle, password_hash, created_at)
		values (1, 'thijs', 'Thijs', '\x01', 'hash', now())`); err != nil {
		t.Fatal(err)
	}

	if _, err := Create(ctx, pool, New{Username: "thijs", PasswordHash: "h", Role: Member}); !errors.Is(err, ErrTaken) {
		t.Fatalf("owner's name: %v", err)
	}
	anna, err := Create(ctx, pool, New{Username: "anna", DisplayName: "Anna", PasswordHash: "h1", Role: Member})
	if err != nil || anna.IsAdmin() || len(anna.UserHandle) != 32 {
		t.Fatalf("create: %+v %v", anna, err)
	}
	if _, err := Create(ctx, pool, New{Username: "anna", PasswordHash: "h", Role: Member}); !errors.Is(err, ErrTaken) {
		t.Fatalf("duplicate: %v", err)
	}

	if a, err := ByUsername(ctx, pool, "thijs"); err != nil || !a.IsOwner() || !a.IsAdmin() {
		t.Fatalf("owner by name: %+v %v", a, err)
	}
	if a, err := ByHandle(ctx, pool, anna.UserHandle); err != nil || a.ID != anna.ID {
		t.Fatalf("by handle: %+v %v", a, err)
	}
	admin, disabled := Admin, true
	if a, err := Update(ctx, pool, anna.ID, Change{Role: &admin, Disabled: &disabled}); err != nil || !a.IsAdmin() || !a.Disabled {
		t.Fatalf("update: %+v %v", a, err)
	}
	if n, err := Admins(ctx, pool); err != nil || n != 1 {
		t.Fatalf("admins = %d %v (a disabled admin does not count)", n, err)
	}
	if err := SetPassword(ctx, pool, OwnerID, "new"); err != nil {
		t.Fatal(err)
	}
	if o, _ := Owner(ctx, pool); o.PasswordHash != "new" {
		t.Fatalf("owner hash = %q", o.PasswordHash)
	}
	list, err := List(ctx, pool)
	if err != nil || len(list) != 2 || !list[0].IsOwner() || list[1].Username != "anna" {
		t.Fatalf("list = %+v %v", list, err)
	}
	if err := Delete(ctx, pool, anna.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(ctx, pool, anna.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}
