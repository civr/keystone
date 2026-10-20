// keystone-core is the Keystone API server.
//
//	keystone-core [serve]        run the HTTP server (default)
//	keystone-core reset-network  clear the login/admin network allowlists
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/httpapi"
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

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

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
	default:
		fmt.Fprintf(os.Stderr, "usage: keystone-core [serve|reset-network]\n")
		os.Exit(2)
	}
	if err != nil {
		slog.Error("keystone-core: "+cmd, "err", err)
		os.Exit(1)
	}
}

// openDB loads the secrets, connects to Postgres and applies migrations.
func openDB(ctx context.Context) (secrets.Secrets, *pgxpool.Pool, error) {
	sec, err := secrets.Load(env("KEYSTONE_SECRETS", "/run/keystone"))
	if err != nil {
		return sec, nil, err
	}
	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(dbUser, sec.DBPassword),
		Host:     net.JoinHostPort(env("KEYSTONE_DB_HOST", "postgres"), "5432"),
		Path:     "/" + dbName,
		RawQuery: "sslmode=disable",
	}
	pool, err := db.Open(ctx, dsn.String())
	if err != nil {
		return sec, nil, err
	}
	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		return sec, nil, err
	}
	return sec, pool, nil
}

func serve(ctx context.Context) error {
	sec, pool, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{Addr: env("KEYSTONE_VALKEY_ADDR", "valkey:6379"), Password: sec.ValkeyPassword})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("valkey: %w", err)
	}

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
		Dir:           directory(pool, sec.DataKey),
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

// directory returns the AD client for the stored configuration, or nil when
// AD has not been configured yet. It reads ad_config on every call so changes
// made in setup or Instellingen apply immediately.
func directory(pool *pgxpool.Pool, dataKey []byte) func() ad.Directory {
	return func() ad.Directory {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var raw []byte
		err := pool.QueryRow(ctx, `select data from ad_config where id = 1`).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			slog.Error("ad: load config", "err", err)
			return nil
		}
		var cfg ad.Config
		if err := json.Unmarshal(raw, &cfg); err != nil {
			slog.Error("ad: decode config", "err", err)
			return nil
		}
		return ad.NewLDAP(cfg, dataKey)
	}
}

func resetNetwork(ctx context.Context) error {
	_, pool, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := settings.NewStore(pool).ResetNetwork(ctx); err != nil {
		return err
	}
	aw := audit.NewWriter(pool, nil)
	defer aw.Close()
	aw.Log(ctx, audit.ActorCLI, "network.reset", netip.Addr{}, map[string]any{"cleared": []string{"login_cidrs", "admin_cidrs"}})
	fmt.Println("Netwerkbeperkingen gewist: login_cidrs en admin_cidrs zijn leeg.")
	return nil
}
