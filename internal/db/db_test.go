package db

import (
	"context"
	"testing"

	"github.com/civr/keystone/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := testutil.Postgres(t)
	pool, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestMigrateTwiceIsNoop(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("schema_migrations rows = %d, want 2", n)
	}
}

func TestMigrateCreatesTables(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"settings", "ad_config", "owner", "passkeys", "recovery_codes",
		"emergency", "apps", "audit_log", "setup_state", "schema_migrations"} {
		var ok bool
		err := pool.QueryRow(ctx, `select to_regclass('public.'||$1) is not null`, tbl).Scan(&ok)
		if err != nil || !ok {
			t.Errorf("table %s missing (err=%v)", tbl, err)
		}
	}
}
