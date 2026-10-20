package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/passkey"
	"github.com/civr/keystone/internal/settings"
)

const (
	// waTTL bounds a WebAuthn ceremony (begin → finish).
	waTTL               = 5 * time.Minute
	maxPasskeyNameRunes = 64
)

var (
	errNoCeremony  = errors.New("no pending webauthn ceremony")
	errNoPublicURL = errors.New("public_url is not configured")
)

// passkeyService builds the WebAuthn service from the current settings
// (Ruling R4): rpID is the host of public_url, the origin is public_url's origin.
func (s *server) passkeyService(cfg settings.Settings) (*passkey.Service, error) {
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || u.Host == "" {
		return nil, errNoPublicURL
	}
	return passkey.NewService(s.d.Pool, u.Hostname(), u.Scheme+"://"+u.Host, cfg.Login.UserVerification)
}

// putCeremony stores WebAuthn ceremony state under wa:<id> and returns id.
func (s *server) putCeremony(ctx context.Context, state []byte) (string, error) {
	id, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := s.d.Valkey.Set(ctx, "wa:"+id, state, waTTL).Err(); err != nil {
		return "", err
	}
	return id, nil
}

// takeCeremony returns and deletes the state stored under wa:<id>, so each
// challenge can be answered once.
func (s *server) takeCeremony(ctx context.Context, id string) ([]byte, error) {
	if id == "" {
		return nil, errNoCeremony
	}
	state, err := s.d.Valkey.GetDel(ctx, "wa:"+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, errNoCeremony
	}
	return state, err
}

// passkeyName is the ?name= of a registration finish, trimmed and capped;
// "Passkey" when empty.
func passkeyName(r *http.Request) string {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		return "Passkey"
	}
	if utf8.RuneCountInString(name) > maxPasskeyNameRunes {
		name = string([]rune(name)[:maxPasskeyNameRunes])
	}
	return name
}

// bindCeremony stores ceremony state and records "<kind>:<id>" under
// wabind:<bind>, so the ceremony belongs to one pending login or session; a
// new begin replaces the previous binding.
func (s *server) bindCeremony(ctx context.Context, bind, kind string, state []byte) error {
	id, err := s.putCeremony(ctx, state)
	if err != nil {
		return err
	}
	return s.d.Valkey.Set(ctx, "wabind:"+bind, kind+":"+id, waTTL).Err()
}

// takeBoundCeremony returns (and deletes) the ceremony bound to bind and its kind.
func (s *server) takeBoundCeremony(ctx context.Context, bind string) (string, []byte, error) {
	v, err := s.d.Valkey.GetDel(ctx, "wabind:"+bind).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil, errNoCeremony
	}
	if err != nil {
		return "", nil, err
	}
	kind, id, _ := strings.Cut(v, ":")
	state, err := s.takeCeremony(ctx, id)
	return kind, state, err
}
