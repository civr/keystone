package passkey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// owner is the single Keystone account as a WebAuthn user.
type owner struct {
	username, displayName string
	handle                []byte
	creds                 []webauthn.Credential
}

func (o *owner) WebAuthnID() []byte                         { return o.handle }
func (o *owner) WebAuthnName() string                       { return o.username }
func (o *owner) WebAuthnDisplayName() string                { return o.displayName }
func (o *owner) WebAuthnCredentials() []webauthn.Credential { return o.creds }

// store reads and writes the owner and passkeys tables.
type store struct{ pool *pgxpool.Pool }

// loadOwner returns the owner with all registered credentials.
func (s store) loadOwner(ctx context.Context) (*owner, error) {
	var o owner
	var display *string
	err := s.pool.QueryRow(ctx, `select username, display_name, user_handle from owner where id = 1`).
		Scan(&o.username, &display, &o.handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoOwner
	}
	if err != nil {
		return nil, fmt.Errorf("passkey: load owner: %w", err)
	}
	o.displayName = o.username
	if display != nil && *display != "" {
		o.displayName = *display
	}
	rows, err := s.pool.Query(ctx, `select id, public_key, sign_count, aaguid, coalesce(transports, '{}'), flags from passkeys order by created_at`)
	if err != nil {
		return nil, fmt.Errorf("passkey: load credentials: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c webauthn.Credential
		var signCount int64
		var transports []string
		var flags int16
		if err := rows.Scan(&c.ID, &c.PublicKey, &signCount, &c.Authenticator.AAGUID, &transports, &flags); err != nil {
			return nil, fmt.Errorf("passkey: scan credential: %w", err)
		}
		c.Authenticator.SignCount = uint32(signCount)
		c.Flags = webauthn.NewCredentialFlags(protocol.AuthenticatorFlags(flags))
		for _, t := range transports {
			c.Transport = append(c.Transport, protocol.AuthenticatorTransport(t))
		}
		o.creds = append(o.creds, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("passkey: load credentials: %w", err)
	}
	return &o, nil
}

func (s store) add(ctx context.Context, c *webauthn.Credential, name string) error {
	transports := make([]string, 0, len(c.Transport))
	for _, t := range c.Transport {
		transports = append(transports, string(t))
	}
	_, err := s.pool.Exec(ctx, `insert into passkeys (id, name, public_key, sign_count, aaguid, transports, flags, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		c.ID, name, c.PublicKey, int64(c.Authenticator.SignCount), c.Authenticator.AAGUID, transports,
		int16(c.Flags.ProtocolValue()), time.Now())
	if err != nil {
		return fmt.Errorf("passkey: store credential: %w", err)
	}
	return nil
}

// recordUse stores the new counter and flags after a successful assertion.
// The update only applies when the counter still moves forward (or the
// authenticator does not count at all), so two concurrent replays of the same
// assertion cannot both succeed.
func (s store) recordUse(ctx context.Context, c *webauthn.Credential) error {
	tag, err := s.pool.Exec(ctx, `update passkeys set sign_count = $2, flags = $3, last_used_at = now()
		where id = $1 and (sign_count < $2 or (sign_count = 0 and $2 = 0))`,
		c.ID, int64(c.Authenticator.SignCount), int16(c.Flags.ProtocolValue()))
	if err != nil {
		return fmt.Errorf("passkey: record use: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrSignCount
	}
	return nil
}

func (s store) count(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `select count(*) from passkeys`).Scan(&n); err != nil {
		return 0, fmt.Errorf("passkey: count: %w", err)
	}
	return n, nil
}
