// keystone-core is the Keystone API server.
//
//	keystone-core [serve]        run the HTTP server (default)
//	keystone-core reset-network  clear the login/admin network allowlists
//	keystone-core print-defaults print the factory settings as JSON
//	keystone-core healthcheck    exit 0 if the local server answers /api/healthz
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/httpapi"
	"github.com/civr/keystone/internal/runtime"
	"github.com/civr/keystone/internal/secrets"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

const (
	listenAddr      = ":8080"
	dbUser          = "keystone"
	dbName          = "keystone"
	shutdownTimeout = 15 * time.Second
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx)
	case "reset-network":
		err = resetNetwork(ctx)
	case "print-defaults":
		err = printDefaults(os.Stdout)
	case "healthcheck":
		err = healthcheck(ctx)
	default:
		fmt.Fprintf(os.Stderr, "usage: keystone-core [serve|reset-network|print-defaults|healthcheck]\n")
		os.Exit(2)
	}
	if err != nil {
		slog.Error("keystone-core: "+cmd, "err", err)
		os.Exit(1)
	}
}

// openDB loads the secrets, connects to Postgres and applies migrations.
func openDB(ctx context.Context) (secrets.Secrets, *pgxpool.Pool, error) {
	return runtime.OpenDB(ctx, true)
}

func serve(ctx context.Context) error {
	sec, pool, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb, err := runtime.Valkey(ctx, sec)
	if err != nil {
		return err
	}
	defer rdb.Close()

	st := settings.NewStore(pool)
	aw := audit.NewWriter(pool, st)
	defer aw.Close()
	go aw.RunPurge(ctx, st)

	setupHash, err := setupCode(ctx, pool)
	if err != nil {
		return err
	}

	handler := httpapi.New(httpapi.Deps{
		Pool:          pool,
		Settings:      st,
		Valkey:        rdb,
		Sessions:      session.NewStore(rdb),
		Limiter:       session.NewLimiter(rdb),
		Dir:           runtime.Directory(pool, sec.DataKey),
		Audit:         aw,
		Secrets:       sec,
		Shell:         httpapi.EmbeddedShell,
		SetupCodeHash: setupHash,
	})
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("keystone-core listening", "addr", listenAddr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("keystone-core shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// setupCode generates a fresh setup code while setup is incomplete, logs it
// and returns its argon2 hash (only the hash stays in memory). It returns ""
// once setup is complete.
func setupCode(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var done bool
	err := pool.QueryRow(ctx, `select completed_at is not null from setup_state where id = 1`).Scan(&done)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("setup state: %w", err)
	}
	if done {
		return "", nil
	}
	code := crypto.RecoveryCode()
	hash, err := crypto.HashSecret(code)
	if err != nil {
		return "", err
	}
	slog.Info("Keystone setup code: " + code)
	return hash, nil
}

func resetNetwork(ctx context.Context) error {
	_, pool, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	return runResetNetwork(ctx, pool, os.Stdout)
}

// runResetNetwork clears both allowlists and audits the reset as actor cli.
func runResetNetwork(ctx context.Context, pool *pgxpool.Pool, out io.Writer) error {
	if err := settings.NewStore(pool).ResetNetwork(ctx); err != nil {
		return err
	}
	aw := audit.NewWriter(pool, nil)
	defer aw.Close()
	aw.Log(ctx, audit.ActorCLI, "settings.network_reset", netip.Addr{}, map[string]any{"cleared": []string{"login_cidrs", "admin_cidrs"}})
	_, err := fmt.Fprintln(out, "Netwerkbeperkingen gewist: login_cidrs en admin_cidrs zijn leeg.")
	return err
}

// printDefaults writes settings.Defaults() as indented JSON. The web UI's
// settingsMeta test uses this output (web/src/lib/fixtures/defaults.json) to
// prove every setting has a label.
func printDefaults(out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(settings.Defaults())
}
