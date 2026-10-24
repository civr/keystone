// Package ldapsrv is keystone-ldap (spec §2, §4): a small LDAP server for apps
// that only speak LDAP (Jellyfin). Each LDAP app gets its own subtree
// ou=<slug>,dc=keystone holding one user, the app account mapping. A user
// bind checks the password against AD as the owner; there is no cached
// fallback when AD is down (§9).
//
// Supported: simple Bind, Search, Unbind. Everything else is refused.
package ldapsrv

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"time"

	ber "github.com/go-asn1-ber/asn1-ber"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/runtime"
	"github.com/civr/keystone/internal/session"
)

// BaseDN is the root of Keystone's tree.
const BaseDN = "dc=keystone"

const (
	maxPacket   = 1 << 20
	idleTimeout = 5 * time.Minute
	opTimeout   = 10 * time.Second
	maxResults  = 16
)

func init() { ber.MaxPacketLengthBytes = maxPacket }

// Source supplies the current settings, apps and owner.
type Source interface {
	Get(ctx context.Context) (runtime.View, error)
}

// Server serves LDAP connections.
type Server struct {
	View    Source
	Dir     func() ad.Directory
	Limiter *session.Limiter
	Audit   *audit.Writer // nil in some tests
}

// Serve accepts connections on ln. plain marks a listener without TLS: its
// connections are closed while ldap.require_tls is on.
func (s *Server) Serve(ctx context.Context, ln net.Listener, plain bool) error {
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go s.handle(ctx, c, plain)
	}
}

// bound is the authentication state of a connection.
type bound struct {
	slug string // app subtree the connection may read; "" = not bound
	user bool   // bound as the app's user rather than its service account
}

type conn struct {
	s   *Server
	c   net.Conn
	ip  netip.Addr
	w   *bufio.Writer
	who bound
}

func (s *Server) handle(ctx context.Context, c net.Conn, plain bool) {
	defer c.Close()
	ap, _ := netip.ParseAddrPort(c.RemoteAddr().String())
	cn := &conn{s: s, c: c, ip: ap.Addr().Unmap(), w: bufio.NewWriter(c)}
	v, err := s.View.Get(ctx)
	if err != nil {
		slog.Error("ldap: load settings", "err", err)
		return
	}
	if plain && v.Settings.LDAP.RequireTLS {
		return
	}
	if !allowed(v.Settings.LDAP.AllowedCIDRs, cn.ip) {
		slog.Warn("ldap: client not allowed", "ip", cn.ip.String())
		return
	}
	if tc, ok := c.(*tls.Conn); ok {
		_ = tc.SetDeadline(time.Now().Add(opTimeout))
		if err := tc.Handshake(); err != nil {
			return
		}
	}
	for {
		_ = c.SetReadDeadline(time.Now().Add(idleTimeout))
		p, err := ber.ReadPacket(c)
		if err != nil {
			return
		}
		_ = c.SetDeadline(time.Now().Add(opTimeout))
		if !cn.dispatch(ctx, p) {
			return
		}
		if err := cn.w.Flush(); err != nil {
			return
		}
	}
}

func allowed(cidrs []string, ip netip.Addr) bool {
	if len(cidrs) == 0 {
		return true
	}
	for _, c := range cidrs {
		if p, err := netip.ParsePrefix(c); err == nil && p.Masked().Contains(ip) {
			return true
		}
	}
	return false
}

func (cn *conn) send(b []byte) { _, _ = cn.w.Write(b) }

// dispatch handles one message; false closes the connection.
func (cn *conn) dispatch(ctx context.Context, p *ber.Packet) bool {
	if len(p.Children) < 2 || p.Children[1].ClassType != ber.ClassApplication {
		return false
	}
	id, op := num(p.Children[0]), p.Children[1]
	switch op.Tag {
	case opBindRequest:
		cn.bind(ctx, id, op)
	case opUnbindRequest:
		return false
	case opSearchRequest:
		cn.search(ctx, id, op)
	case opAbandonRequest:
	case opExtendedRequest:
		cn.send(result(id, opExtendedRequest+1, resultProtocolError, "extended operations are not supported"))
	case opModifyRequest, opAddRequest, opDelRequest, opModDNRequest, opCompareRequest:
		cn.send(result(id, op.Tag+1, resultUnwilling, "keystone-ldap is read-only"))
	default:
		return false
	}
	return true
}

// parseDN splits "<attr>=<value>,ou=<slug>,dc=keystone".
func parseDN(dn string) (attr, value, slug string, ok bool) {
	parts := strings.Split(norm(dn), ",")
	if len(parts) != 3 || parts[2] != BaseDN || !strings.HasPrefix(parts[1], "ou=") {
		return "", "", "", false
	}
	kv := strings.SplitN(parts[0], "=", 2)
	if len(kv) != 2 || kv[1] == "" {
		return "", "", "", false
	}
	return kv[0], kv[1], strings.TrimPrefix(parts[1], "ou="), true
}

func appBySlug(v runtime.View, slug string) (apps.App, bool) {
	for _, a := range v.Apps {
		if a.Protocol == apps.LDAPP && a.Config.LDAP != nil && a.Config.LDAP.Slug == slug {
			return a, true
		}
	}
	return apps.App{}, false
}

func (cn *conn) bind(ctx context.Context, id int64, op *ber.Packet) {
	reply := func(code int, diag string) { cn.send(result(id, opBindResponse, code, diag)) }
	cn.who = bound{}
	if len(op.Children) < 3 || num(op.Children[0]) != 3 {
		reply(resultProtocolError, "LDAPv3 only")
		return
	}
	name, auth := str(op.Children[1]), op.Children[2]
	if auth.ClassType != ber.ClassContext || auth.Tag != 0 {
		reply(resultAuthMethodNotSupp, "simple bind only")
		return
	}
	password := str(auth)
	if name == "" && password == "" {
		reply(resultSuccess, "") // anonymous: may only read the root DSE
		return
	}
	v, err := cn.s.View.Get(ctx)
	if err != nil {
		reply(resultUnavailable, "")
		return
	}
	cfg := v.Settings
	if ok, err := cn.s.Limiter.Allow(ctx, "ldap-ip", cn.ip.String(), cfg.LDAP.BindPerIPPerMinute); err != nil || !ok {
		reply(resultBusy, "too many binds, try again later")
		return
	}
	attr, val, slug, ok := parseDN(name)
	a, found := appBySlug(v, slug)
	if !ok || !found || password == "" {
		cn.failed(ctx, name, "unknown dn")
		reply(resultInvalidCredentials, "")
		return
	}
	switch {
	case attr == "cn" && val == "bind":
		h := a.Config.LDAP.BindSecretHash
		if h == "" || !crypto.VerifySecret(h, password) {
			cn.failed(ctx, name, "service password")
			reply(resultInvalidCredentials, "")
			return
		}
		cn.who = bound{slug: slug}
		reply(resultSuccess, "")
	case attr == "uid" && strings.EqualFold(val, a.Mapped(v.Owner).Username):
		cn.userBind(ctx, reply, v, a, name, password)
	default:
		cn.failed(ctx, name, "unknown user")
		reply(resultInvalidCredentials, "")
	}
}

func (cn *conn) userBind(ctx context.Context, reply func(int, string), v runtime.View, a apps.App, dn, password string) {
	account := "ldap:" + a.Config.LDAP.Slug
	if locked, _, err := cn.s.Limiter.Locked(ctx, account); err != nil || locked {
		reply(resultUnwilling, "account temporarily locked")
		return
	}
	var err error
	if v.Owner.PasswordHash != "" {
		// Owner from the short setup: the Keystone password, no AD.
		if !crypto.VerifySecret(v.Owner.PasswordHash, password) {
			err = ad.ErrInvalidCredentials
		}
	} else {
		dir := cn.s.Dir()
		if dir == nil || v.Owner.Username == "" {
			reply(resultUnavailable, "directory not configured")
			return
		}
		actx, cancel := context.WithTimeout(ctx, opTimeout)
		defer cancel()
		_, err = dir.Authenticate(actx, v.Owner.Username, password)
	}
	switch {
	case err == nil:
		_ = cn.s.Limiter.ResetFailures(ctx, account)
		cn.who = bound{slug: a.Config.LDAP.Slug, user: true}
		cn.audit(ctx, "app.access", map[string]any{"app": a.Name, "id": a.ID, "protocol": "ldap"})
		reply(resultSuccess, "")
	case errors.Is(err, ad.ErrInvalidCredentials), errors.Is(err, ad.ErrNotFound):
		_, _ = cn.s.Limiter.Fail(ctx, account, v.Settings)
		cn.failed(ctx, dn, "password")
		reply(resultInvalidCredentials, "")
	case errors.Is(err, ad.ErrUnavailable):
		reply(resultUnavailable, "Active Directory is unreachable")
	default:
		slog.Error("ldap: authenticate", "err", err)
		reply(resultOther, "")
	}
}

func (cn *conn) failed(ctx context.Context, dn, why string) {
	slog.Info("ldap: bind failed", "dn", dn, "ip", cn.ip.String(), "why", why)
	cn.audit(ctx, "ldap.bind_failed", map[string]any{"dn": dn, "reason": why})
}

func (cn *conn) audit(ctx context.Context, action string, detail map[string]any) {
	if cn.s.Audit != nil {
		cn.s.Audit.Log(ctx, audit.ActorOwner, action, cn.ip, detail)
	}
}

// entries returns the objects of the app subtree slug.
func entries(v runtime.View, a apps.App) []entry {
	acct := a.Mapped(v.Owner)
	ou := "ou=" + a.Config.LDAP.Slug + "," + BaseDN
	user := "uid=" + acct.Username + "," + ou
	group := "cn=users," + ou
	sn := acct.DisplayName
	given := ""
	if i := strings.LastIndexByte(sn, ' '); i > 0 {
		given, sn = sn[:i], sn[i+1:]
	}
	if sn == "" {
		sn = acct.Username
	}
	u := entry{DN: user, Attrs: map[string][]string{
		"objectClass": {"top", "person", "organizationalPerson", "inetOrgPerson"},
		"uid":         {acct.Username},
		"cn":          {acct.Username},
		"sn":          {sn},
		"displayName": {acct.DisplayName},
		"memberOf":    {group},
	}}
	if acct.Email != "" {
		u.Attrs["mail"] = []string{acct.Email}
	}
	if given != "" {
		u.Attrs["givenName"] = []string{given}
	}
	return []entry{
		{DN: ou, Attrs: map[string][]string{"objectClass": {"top", "organizationalUnit"}, "ou": {a.Config.LDAP.Slug}}},
		u,
		{DN: group, Attrs: map[string][]string{"objectClass": {"top", "groupOfNames"}, "cn": {"users"}, "member": {user}}},
	}
}

func rootDSE() entry {
	return entry{DN: "", Attrs: map[string][]string{
		"objectClass":          {"top"},
		"namingContexts":       {BaseDN},
		"supportedLDAPVersion": {"3"},
		"vendorName":           {"Keystone"},
	}}
}

// inScope reports whether dn lies within base under scope (0 base, 1 one, 2 sub).
func inScope(dn, base string, scope int64) bool {
	dn, base = norm(dn), norm(base)
	switch scope {
	case 0:
		return dn == base
	case 1:
		i := strings.IndexByte(dn, ',')
		return i > 0 && dn[i+1:] == base
	default:
		return dn == base || strings.HasSuffix(dn, ","+base)
	}
}

func (cn *conn) search(ctx context.Context, id int64, op *ber.Packet) {
	done := func(code int, diag string) { cn.send(result(id, opSearchDone, code, diag)) }
	if len(op.Children) < 8 {
		done(resultProtocolError, "")
		return
	}
	base, scope, typesOnly, filter := str(op.Children[0]), num(op.Children[1]), num(op.Children[5]) == 1, op.Children[6]
	var want []string
	for _, a := range op.Children[7].Children {
		want = append(want, str(a))
	}
	if norm(base) == "" && scope == 0 {
		if e := rootDSE(); match(filter, e, 0) {
			cn.send(encodeEntry(id, e, want, typesOnly))
		}
		done(resultSuccess, "")
		return
	}
	if cn.who.slug == "" {
		done(resultInsufficientAccess, "bind first")
		return
	}
	v, err := cn.s.View.Get(ctx)
	if err != nil {
		done(resultUnavailable, "")
		return
	}
	a, ok := appBySlug(v, cn.who.slug)
	if !ok {
		done(resultInsufficientAccess, "")
		return
	}
	ou := "ou=" + cn.who.slug + "," + BaseDN
	nb := norm(base)
	if nb != BaseDN && nb != ou && !strings.HasSuffix(nb, ","+ou) {
		done(resultNoSuchObject, "")
		return
	}
	n := 0
	for _, e := range entries(v, a) {
		if inScope(e.DN, base, scope) && match(filter, e, 0) && n < maxResults {
			cn.send(encodeEntry(id, e, want, typesOnly))
			n++
		}
	}
	done(resultSuccess, "")
}
