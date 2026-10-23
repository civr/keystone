package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/civr/keystone/internal/passkey/passkeytest"
	"github.com/civr/keystone/internal/settings"
)

// Without https there is no passkey: setup can go on without one, and the
// owner then signs in with the AD password until a passkey is added.
func TestSetupPasskeyLater(t *testing.T) {
	t.Parallel()
	s := newSetupEnv(t)
	c := newClient(t, s.env, "192.168.1.10")
	s.unlock(c)
	s.domain(c)
	s.ad(c)
	s.owner(c)

	c.must("POST", "/api/setup/finish", nil, 409) // passkey step not done or skipped
	c.must("POST", "/api/setup/passkey/skip", nil, 200)
	if _, step := status(t, c); step != "emergency" {
		t.Fatalf("step after skip = %q", step)
	}
	c.must("POST", "/api/setup/finish", nil, 200)

	var me map[string]any
	body(t, c.must("GET", "/api/me", nil, 200).Body, &me)
	if me["passkey_missing"] != true {
		t.Fatalf("me = %v", me)
	}
	// Settings writes work without a passkey (step-up by the recent sign-in).
	var cfg map[string]any
	body(t, c.must("GET", "/api/admin/settings", nil, 200).Body, &cfg)
	login := cfg["login"].(map[string]any)
	if login["passkey_required"] != false {
		t.Fatalf("passkey_required = %v after skip", login["passkey_required"])
	}
	// Requiring a passkey while none exists would lock the owner out.
	login["passkey_required"] = true
	w := c.must("PUT", "/api/admin/settings", cfg, 400)
	if !strings.Contains(w.Body.String(), "login.passkey_required") {
		t.Fatalf("body = %s", w.Body.String())
	}

	// Adding the first passkey ends the password stand-in for this session.
	opts := c.must("POST", "/api/admin/passkeys/begin", nil, 200).Body.Bytes()
	a := passkeytest.NewTestAuthenticator(setupOrigin)
	cred, err := a.Register(opts)
	if err != nil {
		t.Fatal(err)
	}
	c.must("POST", "/api/admin/passkeys/finish?name=iPhone", cred, 200)
	body(t, c.must("GET", "/api/me", nil, 200).Body, &me)
	if me["passkey_missing"] != false {
		t.Fatalf("me after passkey = %v", me)
	}
	sid := c.cookies[SessionCookie]
	sess, err := s.deps.Sessions.Get(context.Background(), sid, settings.Defaults(), time.Now())
	if err != nil || sess.NoPasskey {
		t.Fatalf("session after first passkey = %+v %v", sess, err)
	}

	// Signing in again: password only.
	c2 := newClient(t, s.env, "192.168.1.11")
	r := password(c2, "thijs", "correct horse")
	if r.Code != 200 || r.Body["next"] != "done" {
		t.Fatalf("password login = %d %v", r.Code, r.Body)
	}
}
