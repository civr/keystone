package audit

import (
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PRI for facility authpriv (10) and severity informational (6): 10*8+6.
const syslogPRI = "<86>1 "

const (
	dialTimeout   = 3 * time.Second
	sendTimeout   = 3 * time.Second
	syslogAppName = "keystone"
)

// formatRFC5424 renders one RFC 5424 message. The MSG part is a JSON object
// with the audit fields; the MSGID is the action.
func formatRFC5424(at time.Time, host, actor, action string, ip netip.Addr, detail map[string]any) []byte {
	var ipVal any
	if ip.IsValid() {
		ipVal = ip.Unmap().String()
	}
	if detail == nil {
		detail = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{"actor": actor, "action": action, "ip": ipVal, "detail": detail})
	if err != nil {
		body = []byte(`{}`)
	}
	var b strings.Builder
	b.WriteString(syslogPRI)
	b.WriteString(at.UTC().Format("2006-01-02T15:04:05.000000Z"))
	b.WriteByte(' ')
	b.WriteString(headerField(host, 255))
	b.WriteByte(' ')
	b.WriteString(syslogAppName)
	b.WriteString(" - ") // PROCID
	b.WriteString(headerField(action, 32))
	b.WriteString(" - ") // STRUCTURED-DATA
	b.Write(body)
	return []byte(b.String())
}

// headerField makes s a valid RFC 5424 header token (PRINTUSASCII, no
// spaces, bounded length); empty becomes the NILVALUE "-".
func headerField(s string, max int) string {
	if s == "" {
		return "-"
	}
	out := []byte(s)
	if len(out) > max {
		out = out[:max]
	}
	for i, c := range out {
		if c < 33 || c > 126 {
			out[i] = '_'
		}
	}
	return string(out)
}

type outMsg struct {
	proto, addr string
	msg         []byte
}

// forwarder sends messages from a bounded queue on one goroutine so the
// request path never blocks; when the queue is full messages are dropped and
// counted. Delivery is best-effort: a failed write drops the message and the
// connection is re-dialled for the next one.
type forwarder struct {
	ch      chan outMsg
	dropped atomic.Uint64
	done    chan struct{}
	once    sync.Once

	conn        net.Conn // owned by the run goroutine
	proto, addr string
}

func newForwarder(size int) *forwarder {
	f := &forwarder{ch: make(chan outMsg, size), done: make(chan struct{})}
	go f.run()
	return f
}

// Dropped returns the number of messages discarded because the queue was full.
func (f *forwarder) Dropped() uint64 { return f.dropped.Load() }

func (f *forwarder) send(proto, addr string, msg []byte) {
	select {
	case f.ch <- outMsg{proto: proto, addr: addr, msg: msg}:
	default:
		if f.dropped.Add(1) == 1 {
			slog.Warn("audit: syslog queue full, dropping messages")
		}
	}
}

func (f *forwarder) close() {
	f.once.Do(func() { close(f.done) })
}

func (f *forwarder) run() {
	defer f.disconnect()
	for {
		select {
		case <-f.done:
			return
		case m := <-f.ch:
			if err := f.write(m); err != nil {
				slog.Warn("audit: syslog send failed", "proto", m.proto, "addr", m.addr, "err", err)
				f.disconnect()
			}
		}
	}
}

func (f *forwarder) disconnect() {
	if f.conn != nil {
		_ = f.conn.Close()
		f.conn = nil
	}
}

func (f *forwarder) write(m outMsg) error {
	if f.conn == nil || f.proto != m.proto || f.addr != m.addr {
		f.disconnect()
		c, err := dial(m.proto, m.addr)
		if err != nil {
			return err
		}
		f.conn, f.proto, f.addr = c, m.proto, m.addr
	}
	payload := m.msg
	if m.proto != "udp" { // RFC 6587 octet counting over stream transports
		payload = append([]byte(strconv.Itoa(len(m.msg))+" "), m.msg...)
	}
	_ = f.conn.SetWriteDeadline(time.Now().Add(sendTimeout))
	_, err := f.conn.Write(payload)
	return err
}

func dial(proto, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: dialTimeout}
	switch proto {
	case "tls":
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		return tls.DialWithDialer(d, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	case "tcp":
		return d.Dial("tcp", addr)
	default:
		return d.Dial("udp", addr)
	}
}
