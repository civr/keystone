// Package session implements Valkey-backed sessions, trusted-device cookies and rate limiting.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/settings"
)

// ErrNotFound is returned when a session does not exist or has expired.
var ErrNotFound = errors.New("session not found")

const (
	sessPrefix = "sess:"
	sessIndex  = "sessidx"
)

// Session is the server-side state behind the ks_session cookie.
type Session struct {
	ID                  string
	CreatedAt           time.Time
	LastSeenAt          time.Time
	LastFullAuthAt      time.Time
	LastPasskeyAt       time.Time
	IP                  string
	UserAgent           string
	MustRegisterPasskey bool
	Emergency           bool
}

// Store keeps sessions in Valkey.
type Store struct{ rdb *redis.Client }

// NewStore returns a Store using rdb.
func NewStore(rdb *redis.Client) *Store { return &Store{rdb: rdb} }

func key(id string) string { return sessPrefix + id }

// expiresAt is the moment the session stops being valid under cfg.
// Sliding sessions count from the last activity, fixed ones from creation.
func expiresAt(s Session, cfg settings.Settings) time.Time {
	d := time.Duration(cfg.Session.DurationHours) * time.Hour
	if cfg.Session.Sliding {
		return s.LastSeenAt.Add(d)
	}
	return s.CreatedAt.Add(d)
}

func ttlFor(s Session, cfg settings.Settings, now time.Time) time.Duration {
	ttl := expiresAt(s, cfg).Sub(now)
	if ttl < time.Second {
		ttl = time.Second
	}
	return ttl
}

// Create stores a new session with a fresh random ID.
func (s *Store) Create(ctx context.Context, sess Session, cfg settings.Settings) (Session, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return Session{}, fmt.Errorf("session id: %w", err)
	}
	sess.ID = base64.RawURLEncoding.EncodeToString(buf)
	now := time.Now()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	if sess.LastSeenAt.IsZero() {
		sess.LastSeenAt = sess.CreatedAt
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return Session{}, err
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, key(sess.ID), data, ttlFor(sess, cfg, now))
	pipe.SAdd(ctx, sessIndex, sess.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		return Session{}, fmt.Errorf("store session: %w", err)
	}
	return sess, nil
}

// Get loads a session and checks it against the current settings, so a
// shortened duration or idle timeout applies immediately. Expired sessions are
// deleted. When the session is still valid and sliding (or an idle timeout is
// in force) LastSeenAt is advanced to now and the TTL extended.
func (s *Store) Get(ctx context.Context, id string, cfg settings.Settings, now time.Time) (Session, error) {
	raw, err := s.rdb.Get(ctx, key(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	var sess Session
	if err := json.Unmarshal(raw, &sess); err != nil {
		return Session{}, fmt.Errorf("decode session: %w", err)
	}
	expired := !now.Before(expiresAt(sess, cfg))
	if idle := cfg.Session.IdleTimeoutMinutes; idle > 0 && now.Sub(sess.LastSeenAt) > time.Duration(idle)*time.Minute {
		expired = true
	}
	if expired {
		_ = s.Revoke(ctx, id)
		return Session{}, ErrNotFound
	}
	if cfg.Session.Sliding || cfg.Session.IdleTimeoutMinutes > 0 {
		sess.LastSeenAt = now
		data, err := json.Marshal(sess)
		if err != nil {
			return Session{}, err
		}
		// XX: never resurrect a session revoked concurrently.
		err = s.rdb.SetArgs(ctx, key(id), data, redis.SetArgs{Mode: "XX", TTL: ttlFor(sess, cfg, now)}).Err()
		if err != nil && !errors.Is(err, redis.Nil) {
			return Session{}, fmt.Errorf("slide session: %w", err)
		}
	}
	return sess, nil
}

// Update overwrites an existing session, keeping its TTL.
func (s *Store) Update(ctx context.Context, sess Session) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	err = s.rdb.SetArgs(ctx, key(sess.ID), data, redis.SetArgs{Mode: "XX", KeepTTL: true}).Err()
	if errors.Is(err, redis.Nil) {
		return ErrNotFound
	}
	return err
}

// List returns all live sessions, pruning index entries whose key has expired.
func (s *Store) List(ctx context.Context) ([]Session, error) {
	ids, err := s.rdb.SMembers(ctx, sessIndex).Result()
	if err != nil {
		return nil, err
	}
	out := []Session{}
	if len(ids) == 0 {
		return out, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = key(id)
	}
	vals, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	var stale []any
	for i, v := range vals {
		str, ok := v.(string)
		if !ok {
			stale = append(stale, ids[i])
			continue
		}
		var sess Session
		if err := json.Unmarshal([]byte(str), &sess); err != nil {
			return nil, fmt.Errorf("decode session: %w", err)
		}
		out = append(out, sess)
	}
	if len(stale) > 0 {
		_ = s.rdb.SRem(ctx, sessIndex, stale...).Err()
	}
	return out, nil
}

// Revoke deletes one session.
func (s *Store) Revoke(ctx context.Context, id string) error {
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, key(id))
	pipe.SRem(ctx, sessIndex, id)
	_, err := pipe.Exec(ctx)
	return err
}

// RevokeAll deletes every session.
func (s *Store) RevokeAll(ctx context.Context) error {
	ids, err := s.rdb.SMembers(ctx, sessIndex).Result()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(ids)+1)
	for _, id := range ids {
		keys = append(keys, key(id))
	}
	keys = append(keys, sessIndex)
	return s.rdb.Del(ctx, keys...).Err()
}
