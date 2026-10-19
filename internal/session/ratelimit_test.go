package session

import (
	"context"
	"testing"
	"time"

	"github.com/civr/keystone/internal/settings"
)

func TestLimiterBlocksAfterN(t *testing.T) {
	ctx := context.Background()
	l := NewLimiter(newClient(t))
	l.now = func() time.Time { return t0 }
	for i := 1; i <= 3; i++ {
		ok, err := l.Allow(ctx, "login-ip", "1.2.3.4", 3)
		if err != nil || !ok {
			t.Fatalf("hit %d: %v %v", i, ok, err)
		}
	}
	if ok, _ := l.Allow(ctx, "login-ip", "1.2.3.4", 3); ok {
		t.Fatal("4th hit should be blocked")
	}
	if ok, _ := l.Allow(ctx, "login-ip", "5.6.7.8", 3); !ok {
		t.Fatal("other key must be independent")
	}
	l.now = func() time.Time { return t0.Add(time.Minute) }
	if ok, _ := l.Allow(ctx, "login-ip", "1.2.3.4", 3); !ok {
		t.Fatal("new minute should reset")
	}
}

func TestLockoutAfterThreshold(t *testing.T) {
	ctx := context.Background()
	l := NewLimiter(newClient(t))
	cfg := settings.Defaults() // threshold 10, 15 min
	if locked, _, _ := l.Locked(ctx, "thijs"); locked {
		t.Fatal("locked before any failure")
	}
	for i := 1; i <= 9; i++ {
		locked, err := l.Fail(ctx, "thijs", cfg)
		if err != nil || locked {
			t.Fatalf("fail %d: locked=%v err=%v", i, locked, err)
		}
	}
	locked, err := l.Fail(ctx, "thijs", cfg)
	if err != nil || !locked {
		t.Fatalf("10th fail: locked=%v err=%v", locked, err)
	}
	isLocked, ttl, err := l.Locked(ctx, "thijs")
	if err != nil || !isLocked || ttl <= 14*time.Minute || ttl > 15*time.Minute {
		t.Fatalf("Locked = %v %v %v", isLocked, ttl, err)
	}
	if other, _, _ := l.Locked(ctx, "someone"); other {
		t.Fatal("lock leaked to other account")
	}
}

func TestResetFailures(t *testing.T) {
	ctx := context.Background()
	l := NewLimiter(newClient(t))
	cfg := settings.Defaults()
	for i := 0; i < 9; i++ {
		_, _ = l.Fail(ctx, "a", cfg)
	}
	if err := l.ResetFailures(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if locked, _ := l.Fail(ctx, "a", cfg); locked {
		t.Fatal("counter not reset")
	}
}
