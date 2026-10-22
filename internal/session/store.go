// Package session implements Valkey-backed sessions, trusted-device cookies and rate limiting.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/settings"
)

// ErrNotFound is returned when a session does not exist or has expired.
var ErrNotFound = errors.New("session not found")

const (
	sessPrefix = "sess:"
	sessIndex  = "sessidx"

	// Each session is a Valkey hash with two independently written fields, so
	// sliding (seen) and security updates (data) cannot overwrite each other.
	fieldData = "data" // JSON of Session (LastSeenAt here is not authoritative)
	fieldSeen = "seen" // LastSeenAt as unix nanoseconds
)

// slideScript advances "seen" (never backwards) and resets the TTL; no-op if the session is gone.
var slideScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local cur = tonumber(redis.call('HGET', KEYS[1], 'seen'))
if cur and cur >= tonumber(ARGV[1]) then return 0 end
redis.call('HSET', KEYS[1], 'seen', ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1`)

// updateScript replaces "data" only, keeping seen and TTL; no-op if the session is gone.
var updateScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
redis.call('HSET', KEYS[1], 'data', ARGV[1])
return 1`)

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
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, key(sess.ID), fieldData, data, fieldSeen, sess.LastSeenAt.UnixNano())
	pipe.PExpire(ctx, key(sess.ID), ttlFor(sess, cfg, now))
	pipe.SAdd(ctx, sessIndex, sess.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		return Session{}, fmt.Errorf("store session: %w", err)
	}
	return sess, nil
}

func decode(h map[string]string) (Session, error) {
	var sess Session
	if err := json.Unmarshal([]byte(h[fieldData]), &sess); err != nil {
		return Session{}, fmt.Errorf("decode session: %w", err)
	}
	if n, err := strconv.ParseInt(h[fieldSeen], 10, 64); err == nil {
		sess.LastSeenAt = time.Unix(0, n)
	}
	return sess, nil
}

// Get loads a session and checks it against the current settings, so a
// shortened duration or idle timeout applies immediately. Expired sessions are
// deleted. When the session is still valid and sliding (or an idle timeout is
// in force) LastSeenAt is advanced to now and the TTL extended; only that
// field is written, so concurrent Updates are never reverted.
func (s *Store) Get(ctx context.Context, id string, cfg settings.Settings, now time.Time) (Session, error) {
	sess, _, err := s.GetSliding(ctx, id, cfg, now)
	return sess, err
}

// GetSliding is Get that also returns LastSeenAt as it was before this call
// advanced it, so callers can tell how long the session was idle.
func (s *Store) GetSliding(ctx context.Context, id string, cfg settings.Settings, now time.Time) (Session, time.Time, error) {
	sess, err := s.get(ctx, id, cfg, now)
	if err != nil {
		return Session{}, time.Time{}, err
	}
	prev := sess.LastSeenAt
	if cfg.Session.Sliding || cfg.Session.IdleTimeoutMinutes > 0 {
		sess.LastSeenAt = now
		if err := slideScript.Run(ctx, s.rdb, []string{key(id)}, now.UnixNano(), ttlFor(sess, cfg, now).Milliseconds()).Err(); err != nil && !errors.Is(err, redis.Nil) {
			return Session{}, time.Time{}, fmt.Errorf("slide session: %w", err)
		}
	}
	return sess, prev, nil
}

// Peek is Get without sliding: it checks that the session is still valid but
// does not count as activity. Downstream protocols (OIDC refresh, gate) use it
// so an app cannot keep the Keystone session alive on its own.
func (s *Store) Peek(ctx context.Context, id string, cfg settings.Settings, now time.Time) (Session, error) {
	return s.get(ctx, id, cfg, now)
}

// get loads id and checks expiry under cfg, revoking an expired session.
func (s *Store) get(ctx context.Context, id string, cfg settings.Settings, now time.Time) (Session, error) {
	h, err := s.rdb.HGetAll(ctx, key(id)).Result()
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	if len(h) == 0 {
		return Session{}, ErrNotFound
	}
	sess, err := decode(h)
	if err != nil {
		return Session{}, err
	}
	expired := !now.Before(expiresAt(sess, cfg))
	if idle := cfg.Session.IdleTimeoutMinutes; idle > 0 && now.Sub(sess.LastSeenAt) > time.Duration(idle)*time.Minute {
		expired = true
	}
	if expired {
		_ = s.Revoke(ctx, id)
		return Session{}, ErrNotFound
	}
	return sess, nil
}

// Update overwrites the session's data, keeping its TTL and last-seen time
// (sess.LastSeenAt is ignored).
func (s *Store) Update(ctx context.Context, sess Session) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	n, err := updateScript.Run(ctx, s.rdb, []string{key(sess.ID)}, data).Int()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
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
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, key(id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	var stale []any
	for i, c := range cmds {
		h := c.Val()
		if len(h) == 0 {
			stale = append(stale, ids[i])
			continue
		}
		sess, err := decode(h)
		if err != nil {
			return nil, err
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
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, key(id))
	pipe.SRem(ctx, sessIndex, id)
	_, err := pipe.Exec(ctx)
	return err
}

// RevokeAll deletes every session, found via the index and a SCAN of the
// keyspace so sessions created concurrently with the index read are not missed.
func (s *Store) RevokeAll(ctx context.Context) error {
	ids, err := s.rdb.SMembers(ctx, sessIndex).Result()
	if err != nil {
		return err
	}
	keys := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		keys[key(id)] = struct{}{}
	}
	it := s.rdb.Scan(ctx, 0, sessPrefix+"*", 500).Iterator()
	for it.Next(ctx) {
		keys[it.Val()] = struct{}{}
	}
	if err := it.Err(); err != nil {
		return err
	}
	del := make([]string, 0, len(keys)+1)
	for k := range keys {
		del = append(del, k)
	}
	del = append(del, sessIndex)
	return s.rdb.Del(ctx, del...).Err()
}
