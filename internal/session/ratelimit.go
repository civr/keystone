package session

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/settings"
)

// Limiter implements per-minute rate limits and account lockout on Valkey.
type Limiter struct {
	rdb *redis.Client
	now func() time.Time
}

// NewLimiter returns a Limiter using rdb.
func NewLimiter(rdb *redis.Client) *Limiter { return &Limiter{rdb: rdb, now: time.Now} }

// Allow counts one hit for (scope, key) in the current minute and reports
// whether it is within perMinute.
func (l *Limiter) Allow(ctx context.Context, scope, key string, perMinute int) (bool, error) {
	k := fmt.Sprintf("rl:%s:%s:%d", scope, key, l.now().Unix()/60)
	pipe := l.rdb.Pipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, 2*time.Minute)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return incr.Val() <= int64(perMinute), nil
}

// Fail records a failed login for account. Reaching LockoutThreshold locks the
// account for LockoutMinutes and returns locked=true.
func (l *Limiter) Fail(ctx context.Context, account string, cfg settings.Settings) (bool, error) {
	window := time.Duration(cfg.BruteForce.LockoutMinutes) * time.Minute
	fk := "fail:" + account
	pipe := l.rdb.Pipeline()
	incr := pipe.Incr(ctx, fk)
	pipe.Expire(ctx, fk, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	if incr.Val() < int64(cfg.BruteForce.LockoutThreshold) {
		return false, nil
	}
	pipe = l.rdb.Pipeline()
	pipe.Set(ctx, "lock:"+account, 1, window)
	pipe.Del(ctx, fk) // fresh count once the lock lapses
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// Locked reports whether account is locked and for how much longer.
func (l *Limiter) Locked(ctx context.Context, account string) (bool, time.Duration, error) {
	ttl, err := l.rdb.PTTL(ctx, "lock:"+account).Result()
	if err != nil {
		return false, 0, err
	}
	if ttl <= 0 { // -2 no key, -1 no expiry (never set by us)
		return false, 0, nil
	}
	return true, ttl, nil
}

// ResetFailures clears the failure counter (call after a successful login).
func (l *Limiter) ResetFailures(ctx context.Context, account string) error {
	return l.rdb.Del(ctx, "fail:"+account).Err()
}
