package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/settings"
	"github.com/civr/keystone/internal/testutil"
)

func newClient(t *testing.T) *redis.Client {
	t.Helper()
	opt, err := redis.ParseURL(testutil.Valkey(t))
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func create(t *testing.T, s *Store, cfg settings.Settings) Session {
	t.Helper()
	sess, err := s.Create(context.Background(), Session{CreatedAt: t0, LastSeenAt: t0, LastFullAuthAt: t0, IP: "10.0.0.1"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestCreateGetRoundTrip(t *testing.T) {
	s := NewStore(newClient(t))
	cfg := settings.Defaults()
	sess := create(t, s, cfg)
	if len(sess.ID) != 43 { // 32 bytes base64url, unpadded
		t.Fatalf("id length %d", len(sess.ID))
	}
	got, err := s.Get(context.Background(), sess.ID, cfg, t0.Add(time.Minute))
	if err != nil || got.IP != "10.0.0.1" || !got.CreatedAt.Equal(t0) {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestGetExpiresWhenDurationShortened(t *testing.T) {
	s := NewStore(newClient(t))
	cfg := settings.Defaults()
	cfg.Session.DurationHours = 720
	sess := create(t, s, cfg)
	short := cfg
	short.Session.DurationHours = 1
	if _, err := s.Get(context.Background(), sess.ID, short, t0.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// and it was deleted: even the original cfg no longer finds it
	if _, err := s.Get(context.Background(), sess.ID, cfg, t0.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want deleted, got %v", err)
	}
}

func TestIdleTimeout(t *testing.T) {
	s := NewStore(newClient(t))
	cfg := settings.Defaults()
	cfg.Session.IdleTimeoutMinutes = 30
	sess := create(t, s, cfg)
	if _, err := s.Get(context.Background(), sess.ID, cfg, t0.Add(20*time.Minute)); err != nil {
		t.Fatalf("within idle: %v", err)
	}
	// last seen is now t0+20m; 31 minutes later is idle
	if _, err := s.Get(context.Background(), sess.ID, cfg, t0.Add(51*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSlidingExtends(t *testing.T) {
	s := NewStore(newClient(t))
	cfg := settings.Defaults()
	cfg.Session.DurationHours = 10
	cfg.Session.Sliding = true
	sess := create(t, s, cfg)
	if _, err := s.Get(context.Background(), sess.ID, cfg, t0.Add(8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// 16h after creation: would be expired without sliding, alive because of the touch at 8h
	if _, err := s.Get(context.Background(), sess.ID, cfg, t0.Add(16*time.Hour)); err != nil {
		t.Fatalf("sliding should extend: %v", err)
	}

	fixed := cfg
	fixed.Session.Sliding = false
	sess2 := create(t, s, fixed)
	if _, err := s.Get(context.Background(), sess2.ID, fixed, t0.Add(8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), sess2.ID, fixed, t0.Add(16*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fixed session must not extend, got %v", err)
	}
}

func TestRevokeThenGetNotFound(t *testing.T) {
	s := NewStore(newClient(t))
	cfg := settings.Defaults()
	sess := create(t, s, cfg)
	if err := s.Revoke(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), sess.ID, cfg, t0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListUpdateRevokeAll(t *testing.T) {
	ctx := context.Background()
	s := NewStore(newClient(t))
	cfg := settings.Defaults()
	a, b := create(t, s, cfg), create(t, s, cfg)
	a.Emergency = true
	if err := s.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, a.ID, cfg, t0)
	if err != nil || !got.Emergency {
		t.Fatalf("update lost: %+v %v", got, err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %d %v", len(list), err)
	}
	if err := s.RevokeAll(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ = s.List(ctx); len(list) != 0 {
		t.Fatalf("after RevokeAll: %d", len(list))
	}
	if err := s.Update(ctx, b); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update of revoked session must fail, got %v", err)
	}
}

func BenchmarkGet(b *testing.B) {
	opt, err := redis.ParseURL(testutil.Valkey(b))
	if err != nil {
		b.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	s := NewStore(rdb)
	cfg := settings.Defaults()
	ctx := context.Background()
	sess, err := s.Create(ctx, Session{CreatedAt: t0, LastSeenAt: t0}, cfg)
	if err != nil {
		b.Fatal(err)
	}
	now := t0.Add(time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get(ctx, sess.ID, cfg, now); err != nil {
			b.Fatal(err)
		}
	}
}
