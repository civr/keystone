// Package gatetoken links the parent-domain ks_gate cookie to a Keystone
// session. The token only authorizes keystone-gate checks; it is useless
// against the Keystone API, so apps on sibling subdomains that see the
// cookie cannot take over the session (ks_session stays host-only).
package gatetoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

// Cookie is the gate cookie name.
const Cookie = "ks_gate"

// ErrNotFound means the token is unknown or its session is gone.
var ErrNotFound = errors.New("gatetoken: not found")

func key(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return "gate:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func index(sessionID string) string { return "gateidx:" + sessionID }

// Issue creates a gate token for sessionID.
func Issue(ctx context.Context, rdb *redis.Client, sessionID string, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(buf)
	pipe := rdb.TxPipeline()
	pipe.Set(ctx, key(tok), sessionID, ttl)
	pipe.SAdd(ctx, index(sessionID), key(tok))
	pipe.Expire(ctx, index(sessionID), ttl)
	_, err := pipe.Exec(ctx)
	return tok, err
}

// Resolve returns the live session behind tok. Using an app behind the gate
// counts as activity, so the session slides like any Keystone request.
func Resolve(ctx context.Context, rdb *redis.Client, sessions *session.Store, cfg settings.Settings, tok string, now time.Time) (session.Session, error) {
	if tok == "" {
		return session.Session{}, ErrNotFound
	}
	sid, err := rdb.Get(ctx, key(tok)).Result()
	if errors.Is(err, redis.Nil) {
		return session.Session{}, ErrNotFound
	}
	if err != nil {
		return session.Session{}, err
	}
	sess, err := sessions.Peek(ctx, sid, cfg, now)
	if errors.Is(err, session.ErrNotFound) {
		_ = rdb.Del(ctx, key(tok)).Err()
		return session.Session{}, ErrNotFound
	}
	return sess, err
}

// RevokeSession deletes every gate token of sessionID.
func RevokeSession(ctx context.Context, rdb *redis.Client, sessionID string) error {
	keys, err := rdb.SMembers(ctx, index(sessionID)).Result()
	if err != nil {
		return err
	}
	return rdb.Del(ctx, append(keys, index(sessionID))...).Err()
}
