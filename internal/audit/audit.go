// Package audit records security events in Postgres, purges them after the
// configured retention and optionally forwards them to syslog.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/civr/keystone/internal/settings"
)

// Actor values recorded in audit_log.actor.
const (
	ActorOwner     = "owner"
	ActorCLI       = "cli"
	ActorSystem    = "system"
	ActorAnonymous = "anonymous"
)

const writeTimeout = 5 * time.Second

// Writer appends audit entries. It is safe for concurrent use.
type Writer struct {
	pool     *pgxpool.Pool
	settings *settings.Store // nil disables syslog forwarding
	fwd      *forwarder
	host     string
}

// NewWriter returns a Writer storing entries in pool. When st is non-nil,
// entries are also forwarded to syslog whenever Audit.SyslogEnabled is set.
// Call Close to stop the syslog sender.
func NewWriter(pool *pgxpool.Pool, st *settings.Store) *Writer {
	host, _ := os.Hostname()
	return &Writer{pool: pool, settings: st, fwd: newForwarder(1024), host: host}
}

// Close stops the syslog sender. Entries still queued are dropped.
func (w *Writer) Close() { w.fwd.close() }

// Log records an event. Failures are logged, never returned: auditing must not
// break the request that triggered it. The write survives cancellation of ctx.
// An invalid ip is stored as NULL.
func (w *Writer) Log(ctx context.Context, actor, action string, ip netip.Addr, detail map[string]any) {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		slog.Error("audit: encode detail", "action", action, "err", err)
		raw = []byte(`{}`)
	}
	var ipArg any
	if ip.IsValid() {
		ipArg = ip.Unmap().String()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	now := time.Now()
	if _, err := w.pool.Exec(ctx, `insert into audit_log (at, actor, action, ip, detail) values ($1, $2, $3, $4, $5)`,
		now, actor, action, ipArg, raw); err != nil {
		slog.Error("audit: write", "action", action, "err", err)
	}
	if w.settings == nil {
		return
	}
	cfg, err := w.settings.Get(ctx)
	if err != nil || !cfg.Audit.SyslogEnabled || cfg.Audit.SyslogAddr == "" {
		return
	}
	w.fwd.send(cfg.Audit.SyslogProto, cfg.Audit.SyslogAddr, formatRFC5424(now, w.host, actor, action, ip, detail))
}

// Purge deletes entries older than retentionDays and returns how many were
// removed. retentionDays <= 0 keeps everything.
func (w *Writer) Purge(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	tag, err := w.pool.Exec(ctx, `delete from audit_log where at < now() - make_interval(days => $1)`, retentionDays)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RunPurge purges once immediately and then hourly, using the retention from
// the current settings, until ctx is done.
func (w *Writer) RunPurge(ctx context.Context, st *settings.Store) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if cfg, err := st.Get(ctx); err != nil {
			slog.Error("audit: purge: settings", "err", err)
		} else if n, err := w.Purge(ctx, cfg.Audit.RetentionDays); err != nil {
			slog.Error("audit: purge", "err", err)
		} else if n > 0 {
			slog.Info("audit: purged old entries", "rows", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
