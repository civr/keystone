package httpapi

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/passkey"
	"github.com/civr/keystone/internal/settings"
)

// waTTL bounds a WebAuthn ceremony (begin → finish).
const waTTL = 5 * time.Minute

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
