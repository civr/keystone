package passkey

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/civr/keystone/internal/db"
	"github.com/civr/keystone/internal/testutil"
)

const (
	testRPID   = "keystone.home.lan"
	testOrigin = "https://keystone.home.lan"
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
	handle := make([]byte, 64)
	_, _ = rand.Read(handle)
	if _, err := pool.Exec(ctx, `insert into owner (id, username, display_name, given_name, user_handle, created_at)
		values (1, 'thijs', 'Thijs Willems', 'Thijs', $1, now())`, handle); err != nil {
		t.Fatal(err)
	}
	return pool
}

func register(t *testing.T, svc *Service, a *TestAuthenticator, name string) {
	t.Helper()
	ctx := context.Background()
	opts, state, err := svc.BeginRegistration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := a.Register(opts)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/", bytes.NewReader(cred))
	if err := svc.FinishRegistration(ctx, state, r, name); err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
}

func login(svc *Service, a *TestAuthenticator, discoverable bool) error {
	ctx := context.Background()
	opts, state, err := svc.BeginLogin(ctx, discoverable)
	if err != nil {
		return err
	}
	cred, err := a.Assert(opts)
	if err != nil {
		return err
	}
	return svc.FinishLogin(ctx, state, httptest.NewRequest("POST", "/", bytes.NewReader(cred)))
}

func TestPasskeyRegisterAndLogin(t *testing.T) {
	t.Parallel()
	pool := newPool(t)
	ctx := context.Background()
	svc, err := NewService(pool, testRPID, testOrigin, "required")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := svc.Count(ctx); err != nil || n != 0 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	a := NewTestAuthenticator(testOrigin)
	register(t, svc, a, "MacBook")
	if n, err := svc.Count(ctx); err != nil || n != 1 {
		t.Fatalf("Count after register = %d, %v", n, err)
	}
	var name string
	var signCount int64
	if err := pool.QueryRow(ctx, `select name, sign_count from passkeys where id = $1`, a.CredID).Scan(&name, &signCount); err != nil {
		t.Fatal(err)
	}
	if name != "MacBook" || signCount != 0 {
		t.Fatalf("stored passkey name=%q sign_count=%d", name, signCount)
	}

	// Registering the same authenticator again is excluded / rejected.
	opts, _, err := svc.BeginRegistration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(opts, []byte(b64.EncodeToString(a.CredID))) {
		t.Fatalf("existing credential not in excludeCredentials: %s", opts)
	}

	for _, discoverable := range []bool{false, true} {
		if err := login(svc, a, discoverable); err != nil {
			t.Fatalf("login (discoverable=%v): %v", discoverable, err)
		}
	}
	var lastUsedSet bool
	if err := pool.QueryRow(ctx, `select sign_count, last_used_at is not null from passkeys where id = $1`, a.CredID).Scan(&signCount, &lastUsedSet); err != nil {
		t.Fatal(err)
	}
	if signCount != 2 || !lastUsedSet {
		t.Fatalf("after logins sign_count=%d last_used_at set=%v", signCount, lastUsedSet)
	}

	// An assertion made for another origin (phishing site) is rejected.
	evil := *a
	evil.Origin = "https://keystone.evil.example"
	if err := login(svc, &evil, false); err == nil {
		t.Fatal("assertion with foreign origin accepted")
	}
	// An unknown authenticator is rejected.
	other := NewTestAuthenticator(testOrigin)
	other.UserHandle = a.UserHandle
	if err := login(svc, other, true); err == nil {
		t.Fatal("unknown credential accepted")
	}
}

func TestPasskeyBeginRegistrationWithoutOwner(t *testing.T) {
	t.Parallel()
	pool := newPool(t)
	if _, err := pool.Exec(context.Background(), `delete from owner`); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(pool, testRPID, testOrigin, "required")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.BeginRegistration(context.Background()); !errors.Is(err, ErrNoOwner) {
		t.Fatalf("err = %v, want ErrNoOwner", err)
	}
}

func TestSignCountRegressionRejected(t *testing.T) {
	t.Parallel()
	pool := newPool(t)
	ctx := context.Background()
	svc, err := NewService(pool, testRPID, testOrigin, "required")
	if err != nil {
		t.Fatal(err)
	}
	a := NewTestAuthenticator(testOrigin)
	register(t, svc, a, "YubiKey")
	if err := login(svc, a, false); err != nil { // counter 1
		t.Fatal(err)
	}
	if err := login(svc, a, false); err != nil { // counter 2
		t.Fatal(err)
	}
	a.Counter = 1 // a clone replays with a lower counter → assertion carries 2 again
	if err := login(svc, a, false); !errors.Is(err, ErrSignCount) {
		t.Fatalf("err = %v, want ErrSignCount", err)
	}
	a.Counter = 0 // → 1, lower than stored 2
	if err := login(svc, a, true); !errors.Is(err, ErrSignCount) {
		t.Fatalf("err = %v, want ErrSignCount", err)
	}
	var signCount int64
	if err := pool.QueryRow(ctx, `select sign_count from passkeys where id = $1`, a.CredID).Scan(&signCount); err != nil {
		t.Fatal(err)
	}
	if signCount != 2 {
		t.Fatalf("sign_count = %d after rejected logins, want 2", signCount)
	}
}

func TestNewServiceValidates(t *testing.T) {
	if _, err := NewService(nil, "", testOrigin, "required"); err == nil {
		t.Fatal("empty rpID accepted")
	}
	if _, err := NewService(nil, testRPID, "", "required"); err == nil {
		t.Fatal("empty origin accepted")
	}
	if _, err := NewService(nil, testRPID, testOrigin, "discouraged"); err == nil {
		t.Fatal("uv discouraged accepted")
	}
}
