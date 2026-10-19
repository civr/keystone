package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists settings in Postgres (row id=1) with an in-memory cache.
// It assumes a single core instance.
type Store struct {
	pool  *pgxpool.Pool
	mu    sync.Mutex
	cache *Settings
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Get returns the current settings, seeding Defaults() if the row is missing.
func (s *Store) Get(ctx context.Context) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(ctx)
}

func (s *Store) loadLocked(ctx context.Context) (Settings, error) {
	if s.cache != nil {
		return *s.cache, nil
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, `select data from settings where id = 1`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		d := Defaults()
		if err := s.saveLocked(ctx, d); err != nil {
			return Settings{}, err
		}
		return d, nil
	}
	if err != nil {
		return Settings{}, err
	}
	cur := Defaults() // fields missing from stored JSON keep their defaults
	if err := json.Unmarshal(raw, &cur); err != nil {
		return Settings{}, err
	}
	cur.normalize()
	s.cache = &cur
	return cur, nil
}

func (s *Store) saveLocked(ctx context.Context, v Settings) error {
	v.normalize()
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `insert into settings (id, data, updated_at) values (1, $1, now())
		on conflict (id) do update set data = excluded.data, updated_at = excluded.updated_at`, raw)
	if err != nil {
		s.cache = nil
		return err
	}
	s.cache = &v
	return nil
}

// Put validates, persists next and returns the previous settings.
func (s *Store) Put(ctx context.Context, next Settings, requesterIP netip.Addr) (Settings, error) {
	if err := next.Validate(requesterIP); err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.loadLocked(ctx)
	if err != nil {
		return Settings{}, err
	}
	if err := s.saveLocked(ctx, next); err != nil {
		return Settings{}, err
	}
	return old, nil
}

// ResetNetwork clears login and admin CIDR allowlists (emergency lockout recovery).
func (s *Store) ResetNetwork(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.loadLocked(ctx)
	if err != nil {
		return err
	}
	cur.Network.LoginCIDRs = []string{}
	cur.Network.AdminCIDRs = []string{}
	return s.saveLocked(ctx, cur)
}
