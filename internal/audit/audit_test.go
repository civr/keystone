package audit

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/settings"
	"github.com/civr/keystone/internal/testutil"
)

func newPool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

func TestAuditPurge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := newPool(t)
	w := NewWriter(pool, nil)
	t.Cleanup(w.Close)

	w.Log(ctx, ActorOwner, "auth.login", netip.MustParseAddr("192.168.1.5"), map[string]any{"method": "passkey"})
	w.Log(ctx, ActorAnonymous, "auth.denied", netip.Addr{}, nil)
	for _, age := range []string{"40 days", "400 days"} {
		if _, err := pool.Exec(ctx, `insert into audit_log (at, actor, action) values (now() - $1::interval, 'system', 'old')`, age); err != nil {
			t.Fatal(err)
		}
	}

	var actor, ip string
	var detail []byte
	if err := pool.QueryRow(ctx, `select actor, host(ip), detail from audit_log where action = 'auth.login'`).Scan(&actor, &ip, &detail); err != nil {
		t.Fatal(err)
	}
	if actor != "owner" || ip != "192.168.1.5" || !strings.Contains(string(detail), `"method": "passkey"`) {
		t.Fatalf("logged row = %q %q %s", actor, ip, detail)
	}
	var nullIP bool
	if err := pool.QueryRow(ctx, `select ip is null from audit_log where action = 'auth.denied'`).Scan(&nullIP); err != nil {
		t.Fatal(err)
	}
	if !nullIP {
		t.Fatal("invalid IP should be stored as NULL")
	}

	n, err := w.Purge(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("purged %d rows, want 2", n)
	}
	var left int
	if err := pool.QueryRow(ctx, `select count(*) from audit_log`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 2 {
		t.Fatalf("rows left = %d, want 2", left)
	}

	if n, err := w.Purge(ctx, 0); err != nil || n != 0 {
		t.Fatalf("Purge(0) = %d, %v; want 0 (keep forever)", n, err)
	}
}

func TestAuditLogForwardsToSyslog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := newPool(t)
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	st := settings.NewStore(pool)
	cfg := settings.Defaults()
	cfg.Audit.SyslogEnabled = true
	cfg.Audit.SyslogProto = "udp"
	cfg.Audit.SyslogAddr = conn.LocalAddr().String()
	if _, err := st.Put(ctx, cfg, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	w := NewWriter(pool, st)
	t.Cleanup(w.Close)

	w.Log(ctx, ActorCLI, "network.reset", netip.Addr{}, map[string]any{"by": "test"})

	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("no syslog datagram: %v", err)
	}
	msg := string(buf[:n])
	if !strings.HasPrefix(msg, "<86>1 ") || !strings.Contains(msg, " keystone - network.reset - ") || !strings.Contains(msg, `"actor":"cli"`) {
		t.Fatalf("syslog message = %q", msg)
	}
}

func TestFormatRFC5424(t *testing.T) {
	at := time.Date(2026, 10, 19, 8, 30, 0, 123456000, time.UTC)
	got := string(formatRFC5424(at, "core1", "owner", "auth.login", netip.MustParseAddr("192.168.1.5"), map[string]any{"k": "v"}))
	want := `<86>1 2026-10-19T08:30:00.123456Z core1 keystone - auth.login - {"action":"auth.login","actor":"owner","detail":{"k":"v"},"ip":"192.168.1.5"}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	// Invalid header characters are replaced, empty fields become NILVALUE.
	got = string(formatRFC5424(at, "", "system", "bad action\n", netip.Addr{}, nil))
	if !strings.HasPrefix(got, "<86>1 2026-10-19T08:30:00.123456Z - keystone - bad_action_ - ") {
		t.Fatalf("got %s", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[strings.Index(got, "{"):]), &body); err != nil {
		t.Fatal(err)
	}
	if body["ip"] != nil {
		t.Fatalf("ip = %v, want null", body["ip"])
	}
}

func TestForwarderTCPOctetCounting(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := newForwarder(4)
	t.Cleanup(f.close)
	f.send("tcp", ln.Addr().String(), []byte("hello"))
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "5 hello" {
		t.Fatalf("frame = %q, want %q", got, "5 hello")
	}
}

func TestForwarderDropsWhenFull(t *testing.T) {
	f := &forwarder{ch: make(chan outMsg, 2)} // not running: nothing drains the queue
	for i := 0; i < 5; i++ {
		f.send("udp", "127.0.0.1:1", []byte("x"))
	}
	if got := f.Dropped(); got != 3 {
		t.Fatalf("Dropped = %d, want 3", got)
	}
}
