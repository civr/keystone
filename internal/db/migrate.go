package db

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrateLockID is the pg_advisory_lock key guarding concurrent migrators.
const migrateLockID int64 = 0x6b657973746f6e65 // "keystone"

// Migrate applies embedded migrations in filename order, each in its own
// transaction, recording them in schema_migrations. Already-applied versions
// are skipped.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// Advisory locks are per-session, so hold one dedicated connection.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, migrateLockID); err != nil {
		return fmt.Errorf("migrate: lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1)`, migrateLockID)
	}()

	if _, err := conn.Exec(ctx, `create table if not exists schema_migrations (version text primary key)`); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var done bool
		if err := conn.QueryRow(ctx, `select exists(select 1 from schema_migrations where version=$1)`, version).Scan(&done); err != nil {
			return fmt.Errorf("migrate: check %s: %w", version, err)
		}
		if done {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err := applyOne(ctx, conn, version, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(ctx context.Context, conn *pgxpool.Conn, version, body string) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("migrate: begin %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("migrate: apply %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx, `insert into schema_migrations(version) values ($1)`, version); err != nil {
		return fmt.Errorf("migrate: record %s: %w", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrate: commit %s: %w", version, err)
	}
	return nil
}
