package settings

import (
	"context"
	"net/netip"
	"reflect"
	"testing"

	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/testutil"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	pool, err := db.Open(ctx, testutil.Postgres(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool)
}

func TestPutReturnsOldAndInvalidatesCache(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	got, err := st.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Defaults()) {
		t.Fatalf("seeded = %+v", got)
	}
	next := Defaults()
	next.Session.DurationHours = 12
	old, err := st.Put(ctx, next, netip.MustParseAddr("192.168.1.5"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Session.DurationHours != 720 {
		t.Fatalf("old = %d", old.Session.DurationHours)
	}
	got, _ = st.Get(ctx)
	if got.Session.DurationHours != 12 {
		t.Fatalf("cache stale: %d", got.Session.DurationHours)
	}
	// A fresh store (no cache) sees the persisted value.
	fresh := NewStore(st.pool)
	got, _ = fresh.Get(ctx)
	if got.Session.DurationHours != 12 {
		t.Fatalf("not persisted: %d", got.Session.DurationHours)
	}
	// Invalid input is rejected and nothing changes.
	bad := Defaults()
	bad.Session.DurationHours = 0
	if _, err := st.Put(ctx, bad, netip.Addr{}); err == nil {
		t.Fatal("want error")
	}
	got, _ = st.Get(ctx)
	if got.Session.DurationHours != 12 {
		t.Fatal("invalid put changed state")
	}
}

func TestResetNetworkClearsLists(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	s := Defaults()
	s.Network.AdminCIDRs = []string{"192.168.1.0/24"}
	s.Network.LoginCIDRs = []string{"192.168.1.0/24"}
	s.Network.TrustedProxies = []string{"172.16.0.0/12"}
	s.Session.DurationHours = 24
	if _, err := st.Put(ctx, s, netip.MustParseAddr("192.168.1.5")); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetNetwork(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(ctx)
	if len(got.Network.AdminCIDRs) != 0 || len(got.Network.LoginCIDRs) != 0 {
		t.Fatalf("not cleared: %+v", got.Network)
	}
	if len(got.Network.TrustedProxies) != 1 || got.Session.DurationHours != 24 {
		t.Fatalf("collateral change: %+v", got)
	}
	got, _ = NewStore(st.pool).Get(ctx)
	if len(got.Network.AdminCIDRs) != 0 {
		t.Fatal("not persisted")
	}
}
