package ldapsrv

import (
	"context"
	"errors"
	"net"
	"sort"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/runtime"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
	"github.com/civr/keystone/internal/testutil"
)

type fixedView struct{ v runtime.View }

func (f *fixedView) Get(context.Context) (runtime.View, error) { return f.v, nil }

const (
	jfSecret = "jellyfin-service-secret"
	jfBind   = "cn=bind,ou=jellyfin,dc=keystone"
	jfUser   = "uid=t.willems,ou=jellyfin,dc=keystone"
)

type fixture struct {
	view *fixedView
	addr string
	dir  *ad.Fake
}

func hash(s string) string {
	h, err := crypto.HashSecret(s)
	if err != nil {
		panic(err)
	}
	return h
}

func start(t *testing.T) *fixture {
	t.Helper()
	opt, err := redis.ParseURL(testutil.Valkey(t))
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	hash := func(s string) string {
		h, err := crypto.HashSecret(s)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	cfg := settings.Defaults()
	cfg.LDAP.RequireTLS = false
	cfg.LDAP.BindPerIPPerMinute = 1000
	f := &fixture{
		view: &fixedView{runtime.View{
			Settings: cfg,
			Owner:    apps.Owner{Username: "thijs", DisplayName: "Thijs Willems", Email: "thijs@home.lan"},
			Apps: []apps.App{
				{ID: "1", Name: "Jellyfin", Protocol: apps.LDAPP, Config: apps.Config{
					Account: apps.Account{Username: "t.willems"},
					LDAP:    &apps.LDAP{Slug: "jellyfin", BindSecretHash: hash(jfSecret)}}},
				{ID: "2", Name: "Other", Protocol: apps.LDAPP, Config: apps.Config{
					LDAP: &apps.LDAP{Slug: "other", BindSecretHash: hash("other-secret")}}},
			},
		}},
		dir: &ad.Fake{Users: map[string]ad.FakeUser{"thijs": {Password: "correct horse"}}},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv := &Server{View: f.view, Dir: func() ad.Directory { return f.dir }, Limiter: session.NewLimiter(rdb)}
	go func() { _ = srv.Serve(ctx, ln, true) }()
	f.addr = ln.Addr().String()
	return f
}

func (f *fixture) dial(t *testing.T) *ldap.Conn {
	t.Helper()
	c, err := ldap.DialURL("ldap://" + f.addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func code(err error) uint16 {
	var le *ldap.Error
	if errors.As(err, &le) {
		return le.ResultCode
	}
	return 0
}

func search(c *ldap.Conn, base, filter string) ([]*ldap.Entry, error) {
	res, err := c.Search(ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, filter, []string{"uid", "mail", "displayName", "memberOf"}, nil))
	if err != nil {
		return nil, err
	}
	return res.Entries, nil
}

func TestLDAP(t *testing.T) {
	f := start(t)

	t.Run("ServiceBindAndSearchFindsUser", func(t *testing.T) {
		c := f.dial(t)
		if err := c.Bind(jfBind, jfSecret); err != nil {
			t.Fatal(err)
		}
		es, err := search(c, "dc=keystone", "(&(objectClass=inetOrgPerson)(uid=T.Willems))")
		if err != nil || len(es) != 1 || es[0].DN != jfUser {
			t.Fatalf("entries = %v, %v", es, err)
		}
		if es[0].GetAttributeValue("mail") != "thijs@home.lan" || es[0].GetAttributeValue("displayName") != "Thijs Willems" {
			t.Fatalf("attrs = %v", es[0].Attributes)
		}
		es, _ = search(c, "ou=jellyfin,dc=keystone", "(|(uid=t.w*)(mail=nobody))")
		if len(es) != 1 {
			t.Fatalf("substring/or = %d", len(es))
		}
		es, _ = search(c, "dc=keystone", "(memberOf=cn=users, ou=jellyfin, dc=keystone)")
		if len(es) != 1 {
			t.Fatalf("memberOf = %d", len(es))
		}
		es, _ = search(c, "dc=keystone", "(!(uid=t.willems))")
		for _, e := range es {
			if e.DN == jfUser {
				t.Fatal("not filter matched user")
			}
		}
	})

	t.Run("UserBindChecksAD", func(t *testing.T) {
		c := f.dial(t)
		if err := c.Bind(jfUser, "correct horse"); err != nil {
			t.Fatal(err)
		}
		if err := c.Bind(jfUser, "wrong"); code(err) != ldap.LDAPResultInvalidCredentials {
			t.Fatalf("wrong password = %v", err)
		}
		if err := c.Bind("uid=thijs,ou=jellyfin,dc=keystone", "correct horse"); code(err) != ldap.LDAPResultInvalidCredentials {
			t.Fatalf("unmapped name = %v", err)
		}
	})

	t.Run("LocalOwnerPasswordWithoutAD", func(t *testing.T) {
		saved := f.view.v.Owner
		f.view.v.Owner.PasswordHash = hash("keystone-only password")
		defer func() { f.view.v.Owner = saved }()
		down := f.dir.Down
		f.dir.Down = true
		defer func() { f.dir.Down = down }()
		c := f.dial(t)
		if err := c.Bind(jfUser, "keystone-only password"); err != nil {
			t.Fatalf("local password: %v", err)
		}
		if err := c.Bind(jfUser, "correct horse"); code(err) != ldap.LDAPResultInvalidCredentials {
			t.Fatalf("AD password accepted for a local owner: %v", err)
		}
	})

	t.Run("EmptyPasswordRejected", func(t *testing.T) {
		c := f.dial(t)
		err := c.UnauthenticatedBind(jfUser)
		if code(err) != ldap.LDAPResultInvalidCredentials {
			t.Fatalf("unauthenticated bind = %v", err)
		}
	})

	t.Run("CrossAppSearchEmpty", func(t *testing.T) {
		c := f.dial(t)
		if err := c.Bind("cn=bind,ou=other,dc=keystone", "other-secret"); err != nil {
			t.Fatal(err)
		}
		if _, err := search(c, "ou=jellyfin,dc=keystone", "(objectClass=*)"); code(err) != ldap.LDAPResultNoSuchObject {
			t.Fatalf("other subtree = %v", err)
		}
		es, _ := search(c, "dc=keystone", "(uid=t.willems)")
		if len(es) != 0 {
			t.Fatal("saw another app's user")
		}
		if err := c.Bind(jfBind, "other-secret"); code(err) != ldap.LDAPResultInvalidCredentials {
			t.Fatal("secret of app B bound app A")
		}
	})

	t.Run("UnboundSearchRejected", func(t *testing.T) {
		c := f.dial(t)
		if _, err := search(c, "dc=keystone", "(objectClass=*)"); code(err) != ldap.LDAPResultInsufficientAccessRights {
			t.Fatalf("unbound = %v", err)
		}
		res, err := c.Search(ldap.NewSearchRequest("", ldap.ScopeBaseObject, 0, 0, 0, false, "(objectClass=*)", []string{"namingContexts"}, nil))
		if err != nil || len(res.Entries) != 1 || res.Entries[0].GetAttributeValue("namingContexts") != "dc=keystone" {
			t.Fatalf("root DSE = %v %v", res, err)
		}
	})

	t.Run("WritesRefused", func(t *testing.T) {
		c := f.dial(t)
		_ = c.Bind(jfBind, jfSecret)
		if err := c.Del(ldap.NewDelRequest(jfUser, nil)); code(err) != ldap.LDAPResultUnwillingToPerform {
			t.Fatalf("delete = %v", err)
		}
	})

	t.Run("BindLatencyUnder50ms", func(t *testing.T) {
		c := f.dial(t)
		var d []time.Duration
		for range 100 {
			t0 := time.Now()
			if err := c.Bind(jfUser, "correct horse"); err != nil {
				t.Fatal(err)
			}
			d = append(d, time.Since(t0))
		}
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		if p95 := d[94]; p95 > 50*time.Millisecond {
			t.Fatalf("p95 = %v", p95)
		}
	})

	t.Run("LockoutAfterThreshold", func(t *testing.T) {
		c := f.dial(t)
		for range f.view.v.Settings.BruteForce.LockoutThreshold {
			_ = c.Bind(jfUser, "wrong")
		}
		if err := c.Bind(jfUser, "correct horse"); code(err) != ldap.LDAPResultUnwillingToPerform {
			t.Fatalf("after lockout = %v", err)
		}
	})

	t.Run("PlainClosedWhenTLSRequired", func(t *testing.T) {
		f.view.v.Settings.LDAP.RequireTLS = true
		defer func() { f.view.v.Settings.LDAP.RequireTLS = false }()
		c, err := ldap.DialURL("ldap://" + f.addr)
		if err == nil {
			err = c.Bind(jfBind, jfSecret)
			c.Close()
		}
		if err == nil {
			t.Fatal("plain LDAP accepted while require_tls is on")
		}
	})

	t.Run("AllowedCIDRs", func(t *testing.T) {
		f.view.v.Settings.LDAP.AllowedCIDRs = []string{"10.0.0.0/8"}
		defer func() { f.view.v.Settings.LDAP.AllowedCIDRs = []string{} }()
		c, err := ldap.DialURL("ldap://" + f.addr)
		if err == nil {
			err = c.Bind(jfBind, jfSecret)
			c.Close()
		}
		if err == nil {
			t.Fatal("client outside allowed_cidrs accepted")
		}
	})
}
