// Package passkey wraps go-webauthn for Keystone's single owner account and
// stores credentials in the passkeys table.
package passkey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNoOwner means setup has not stored an owner yet.
	ErrNoOwner = errors.New("passkey: no owner configured")
	// ErrSignCount means the authenticator's signature counter did not
	// advance: a possible cloned authenticator or a replayed assertion.
	ErrSignCount = errors.New("passkey: signature counter did not increase")
)

// Service runs WebAuthn ceremonies for one relying party. Build one per
// request from the current settings (Ruling R4) so a changed public_url
// applies immediately.
type Service struct {
	wa    *webauthn.WebAuthn
	store store
	uv    protocol.UserVerificationRequirement
}

// NewService returns a Service for rpID (host of public_url) and rpOrigin
// (origin of public_url). uv is login.user_verification: "required" or "preferred".
func NewService(pool *pgxpool.Pool, rpID, rpOrigin string, uv string) (*Service, error) {
	if rpID == "" || rpOrigin == "" {
		return nil, errors.New("passkey: relying party ID and origin are required")
	}
	req := protocol.UserVerificationRequirement(uv)
	if req != protocol.VerificationRequired && req != protocol.VerificationPreferred {
		return nil, fmt.Errorf("passkey: unsupported user verification %q", uv)
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:                  rpID,
		RPDisplayName:         "Keystone",
		RPOrigins:             []string{rpOrigin},
		AttestationPreference: protocol.PreferNoAttestation,
	})
	if err != nil {
		return nil, fmt.Errorf("passkey: %w", err)
	}
	return &Service{wa: wa, store: store{pool: pool}, uv: req}, nil
}

func marshalCeremony(options any, sd *webauthn.SessionData) (json.RawMessage, []byte, error) {
	opts, err := json.Marshal(options)
	if err != nil {
		return nil, nil, err
	}
	state, err := json.Marshal(sd)
	if err != nil {
		return nil, nil, err
	}
	return opts, state, nil
}

func decodeState(state []byte) (webauthn.SessionData, error) {
	var sd webauthn.SessionData
	if err := json.Unmarshal(state, &sd); err != nil || sd.Challenge == "" {
		return sd, errors.New("passkey: invalid ceremony state")
	}
	return sd, nil
}

// BeginRegistration starts registering a new discoverable passkey for the
// owner. options is the CredentialCreationOptions JSON for the browser; state
// must be kept server-side and passed to FinishRegistration.
func (s *Service) BeginRegistration(ctx context.Context) (json.RawMessage, []byte, error) {
	o, err := s.store.loadOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(o.creds))
	for _, c := range o.creds {
		exclude = append(exclude, c.Descriptor())
	}
	creation, sd, err := s.wa.BeginRegistration(o,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: s.uv,
		}),
		webauthn.WithExclusions(exclude),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("passkey: begin registration: %w", err)
	}
	return marshalCeremony(creation, sd)
}

// FinishRegistration verifies the browser's attestation in r's body against
// state and stores the credential under name.
func (s *Service) FinishRegistration(ctx context.Context, state []byte, r *http.Request, name string) error {
	sd, err := decodeState(state)
	if err != nil {
		return err
	}
	o, err := s.store.loadOwner(ctx)
	if err != nil {
		return err
	}
	cred, err := s.wa.FinishRegistration(o, sd, r)
	if err != nil {
		return fmt.Errorf("passkey: finish registration: %w", err)
	}
	return s.store.add(ctx, cred, name)
}

// BeginLogin starts an assertion. discoverable lets the authenticator pick the
// credential (no allowCredentials list); otherwise the owner's credentials are listed.
func (s *Service) BeginLogin(ctx context.Context, discoverable bool) (json.RawMessage, []byte, error) {
	var (
		assertion *protocol.CredentialAssertion
		sd        *webauthn.SessionData
		err       error
	)
	if discoverable {
		assertion, sd, err = s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(s.uv))
	} else {
		var o *owner
		if o, err = s.store.loadOwner(ctx); err != nil {
			return nil, nil, err
		}
		assertion, sd, err = s.wa.BeginLogin(o, webauthn.WithUserVerification(s.uv))
	}
	if err != nil {
		return nil, nil, fmt.Errorf("passkey: begin login: %w", err)
	}
	return marshalCeremony(assertion, sd)
}

// FinishLogin verifies the assertion in r's body against state, then records
// the new signature counter and last_used_at. A counter that does not move
// forward is rejected with ErrSignCount and nothing is updated.
func (s *Service) FinishLogin(ctx context.Context, state []byte, r *http.Request) error {
	sd, err := decodeState(state)
	if err != nil {
		return err
	}
	o, err := s.store.loadOwner(ctx)
	if err != nil {
		return err
	}
	var cred *webauthn.Credential
	if len(sd.UserID) == 0 {
		cred, err = s.wa.FinishDiscoverableLogin(func(_, userHandle []byte) (webauthn.User, error) {
			if !bytes.Equal(userHandle, o.handle) {
				return nil, errors.New("passkey: unknown user handle")
			}
			return o, nil
		}, sd, r)
	} else {
		cred, err = s.wa.FinishLogin(o, sd, r)
	}
	if err != nil {
		return fmt.Errorf("passkey: finish login: %w", err)
	}
	if cred.Authenticator.CloneWarning {
		return ErrSignCount
	}
	return s.store.recordUse(ctx, cred)
}

// Count returns the number of registered passkeys.
func (s *Service) Count(ctx context.Context) (int, error) { return s.store.count(ctx) }
