package gatetoken

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
	"github.com/civr/keystone/internal/testutil"
)

func TestIssueResolveRevoke(t *testing.T) {
	opt, err := redis.ParseURL(testutil.Valkey(t))
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	ctx, cfg, now := context.Background(), settings.Defaults(), time.Now()
	st := session.NewStore(rdb)
	sess, err := st.Create(ctx, session.Session{LastFullAuthAt: now}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := Issue(ctx, rdb, sess.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(ctx, rdb, st, cfg, tok, now)
	if err != nil || got.ID != sess.ID {
		t.Fatalf("resolve = %v %v", got.ID, err)
	}
	if _, err := Resolve(ctx, rdb, st, cfg, "bogus", now); err != ErrNotFound {
		t.Fatalf("bogus = %v", err)
	}

	t.Run("ResolveDiesWithSession", func(t *testing.T) {
		if err := st.Revoke(ctx, sess.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(ctx, rdb, st, cfg, tok, now); err != ErrNotFound {
			t.Fatalf("after revoke = %v", err)
		}
	})

	t.Run("ResolveDoesNotSlide", func(t *testing.T) {
		idle := cfg
		idle.Session.IdleTimeoutMinutes = 30
		s3, _ := st.Create(ctx, session.Session{LastFullAuthAt: now}, idle)
		t3, _ := Issue(ctx, rdb, s3.ID, time.Hour)
		if _, err := Resolve(ctx, rdb, st, idle, t3, now.Add(20*time.Minute)); err != nil {
			t.Fatalf("within idle = %v", err)
		}
		// An app polling through the gate must not keep the session alive.
		if _, err := Resolve(ctx, rdb, st, idle, t3, now.Add(40*time.Minute)); err != ErrNotFound {
			t.Fatalf("after idle = %v", err)
		}
	})

	t.Run("RevokeSessionDeletesTokens", func(t *testing.T) {
		s2, _ := st.Create(ctx, session.Session{LastFullAuthAt: now}, cfg)
		t2, _ := Issue(ctx, rdb, s2.ID, time.Hour)
		if err := RevokeSession(ctx, rdb, s2.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(ctx, rdb, st, cfg, t2, now); err != ErrNotFound {
			t.Fatalf("after RevokeSession = %v", err)
		}
	})
}
