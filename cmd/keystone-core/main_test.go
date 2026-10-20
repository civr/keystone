package main

import (
	"bytes"
	"context"
	"net/netip"
	"testing"

	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/settings"
	"github.com/civr/keystone/internal/testutil"
)

func TestResetNetworkCLI(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Open(ctx, testutil.Postgres(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := settings.NewStore(pool)
	cur, err := st.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cur.Network.LoginCIDRs = []string{"10.0.0.0/8"}
	cur.Network.AdminCIDRs = []string{"10.0.0.0/8"}
	if _, err := st.Put(ctx, cur, netip.MustParseAddr("10.1.2.3")); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runResetNetwork(ctx, pool, &out); err != nil {
		t.Fatal(err)
	}
	got, err := settings.NewStore(pool).Get(ctx) // fresh store: no stale cache
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Network.LoginCIDRs) != 0 || len(got.Network.AdminCIDRs) != 0 {
		t.Fatalf("network = %+v", got.Network)
	}
	if out.Len() == 0 {
		t.Fatal("no confirmation printed")
	}
	var actor string
	var detail string
	if err := pool.QueryRow(ctx, `select actor, detail::text from audit_log where action = 'settings.network_reset'`).Scan(&actor, &detail); err != nil {
		t.Fatalf("audit entry: %v", err)
	}
	if actor != "cli" {
		t.Fatalf("actor = %q", actor)
	}
}
