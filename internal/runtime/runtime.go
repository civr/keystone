// Package runtime holds the start-up plumbing shared by keystone-core,
// keystone-ldap and keystone-gate: secrets, Postgres, Valkey, the AD
// directory and a short-lived snapshot of settings and apps.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/secrets"
	"github.com/civr/keystone/internal/settings"
)

const (
	dbUser = "keystone"
	dbName = "keystone"
)

// Env returns the environment variable name, or def when unset.
func Env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// OpenDB loads the secrets and connects to Postgres; with migrate it also
// applies migrations (only keystone-core does).
func OpenDB(ctx context.Context, migrate bool) (secrets.Secrets, *pgxpool.Pool, error) {
	sec, err := secrets.Load(Env("KEYSTONE_SECRETS", "/run/keystone"))
	if err != nil {
		return sec, nil, err
	}
	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(dbUser, sec.DBPassword),
		Host:     net.JoinHostPort(Env("KEYSTONE_DB_HOST", "postgres"), "5432"),
		Path:     "/" + dbName,
		RawQuery: "sslmode=disable",
	}
	pool, err := db.Open(ctx, dsn.String())
	if err != nil {
		return sec, nil, err
	}
	if migrate {
		if err := db.Migrate(ctx, pool); err != nil {
			pool.Close()
			return sec, nil, err
		}
	}
	return sec, pool, nil
}

// Valkey connects to Valkey and pings it.
func Valkey(ctx context.Context, sec secrets.Secrets) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{Addr: Env("KEYSTONE_VALKEY_ADDR", "valkey:6379"), Password: sec.ValkeyPassword})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("valkey: %w", err)
	}
	return rdb, nil
}

// Directory returns the AD client for the stored configuration, or nil when
// AD has not been configured yet. It reads ad_config on every call so changes
// made in setup or Instellingen apply immediately.
func Directory(pool *pgxpool.Pool, dataKey []byte) func() ad.Directory {
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

// View is one consistent read of what ldap and gate need per request.
type View struct {
	Settings settings.Settings
	Apps     []apps.App
	Owner    apps.Owner
	// Accounts is everyone who can sign in: the owner first, then the users.
	Accounts []apps.Owner
}

// Account returns the account of a session (UserID; "" is the owner).
func (v View) Account(userID string) (apps.Owner, bool) {
	if userID == "" {
		userID = accounts.OwnerID
	}
	for _, a := range v.Accounts {
		if a.ID == userID {
			return a, !a.Disabled
		}
	}
	return apps.Owner{}, false
}

// Snapshot caches a View for TTL, so hot paths (gate checks, LDAP binds) do
// not hit Postgres per request while changes made in Keystone still apply
// within seconds.
type Snapshot struct {
	Pool *pgxpool.Pool
	TTL  time.Duration

	mu   sync.Mutex
	at   time.Time
	view View
}

// Get returns the cached view, refreshing it when older than TTL. On a
// refresh error it keeps serving the previous view for up to a minute.
func (s *Snapshot) Get(ctx context.Context) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.at.IsZero() && time.Since(s.at) < s.TTL {
		return s.view, nil
	}
	v, err := load(ctx, s.Pool)
	if err != nil {
		if !s.at.IsZero() && time.Since(s.at) < time.Minute {
			slog.Warn("runtime: refresh snapshot", "err", err)
			return s.view, nil
		}
		return View{}, err
	}
	s.view, s.at = v, time.Now()
	return v, nil
}

func load(ctx context.Context, pool *pgxpool.Pool) (View, error) {
	var v View
	var err error
	// A fresh Store: its cache would otherwise never see keystone-core's writes.
	if v.Settings, err = settings.NewStore(pool).Get(ctx); err != nil {
		return v, err
	}
	if v.Apps, err = apps.Load(ctx, pool); err != nil {
		return v, err
	}
	if v.Accounts, err = accounts.List(ctx, pool); err != nil {
		return v, err
	}
	v.Owner, err = apps.LoadOwner(ctx, pool)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil // setup not finished: no owner yet
	}
	return v, err
}
