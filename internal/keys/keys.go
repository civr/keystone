// Package keys keeps the RSA signing keys for SAML, OIDC and LDAPS. Private
// keys are stored in Postgres sealed with data_key (which never enters the
// database), so the GUI can rotate them while the secrets volume stays
// read-only.
package keys

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/civr/keystone/internal/crypto"
)

// Uses of a signing key.
const (
	SAML = "saml"
	OIDC = "oidc"
	LDAP = "ldap"
)

// oidcOverlap is how long a retired OIDC key stays in the JWKS, so tokens
// signed just before a rotation still verify.
const oidcOverlap = 24 * time.Hour

// Key is one signing key with its self-signed certificate.
type Key struct {
	ID        string
	Use       string
	Signer    *rsa.PrivateKey
	Cert      *x509.Certificate
	CertPEM   string
	CreatedAt time.Time
	RetiredAt *time.Time
}

// Store loads and rotates keys.
type Store struct {
	pool    *pgxpool.Pool
	dataKey []byte
}

// New returns a Store sealing keys with dataKey.
func New(pool *pgxpool.Pool, dataKey []byte) *Store { return &Store{pool: pool, dataKey: dataKey} }

const cols = `id, use, key_sealed, cert_pem, created_at, retired_at`

func (s *Store) scan(row pgx.Row) (Key, error) {
	var k Key
	var sealed string
	if err := row.Scan(&k.ID, &k.Use, &sealed, &k.CertPEM, &k.CreatedAt, &k.RetiredAt); err != nil {
		return Key{}, err
	}
	der, err := crypto.Open(s.dataKey, sealed)
	if err != nil {
		return Key{}, fmt.Errorf("keys: unseal %s: %w", k.ID, err)
	}
	if k.Signer, err = x509.ParsePKCS1PrivateKey(der); err != nil {
		return Key{}, fmt.Errorf("keys: parse %s: %w", k.ID, err)
	}
	b, _ := pem.Decode([]byte(k.CertPEM))
	if b == nil {
		return Key{}, fmt.Errorf("keys: bad certificate for %s", k.ID)
	}
	if k.Cert, err = x509.ParseCertificate(b.Bytes); err != nil {
		return Key{}, fmt.Errorf("keys: parse certificate %s: %w", k.ID, err)
	}
	return k, nil
}

// Active returns the active key for use, creating one when there is none.
func (s *Store) Active(ctx context.Context, use string) (Key, error) {
	k, err := s.scan(s.pool.QueryRow(ctx, `select `+cols+` from signing_keys where use = $1 and retired_at is null`, use))
	if !errors.Is(err, pgx.ErrNoRows) {
		return k, err
	}
	return s.replace(ctx, use, false)
}

// Rotate retires the active key of use and returns a fresh one.
func (s *Store) Rotate(ctx context.Context, use string) (Key, error) {
	return s.replace(ctx, use, true)
}

// replace creates a new active key under an advisory lock. Without force it
// returns an existing active key that a concurrent caller created first.
func (s *Store) replace(ctx context.Context, use string, force bool) (Key, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Key{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('keystone.keys.' || $1))`, use); err != nil {
		return Key{}, err
	}
	if !force {
		k, err := s.scan(tx.QueryRow(ctx, `select `+cols+` from signing_keys where use = $1 and retired_at is null`, use))
		if err == nil {
			return k, tx.Commit(ctx)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Key{}, err
		}
	}
	k, sealed, err := s.generate(use)
	if err != nil {
		return Key{}, err
	}
	if _, err := tx.Exec(ctx, `update signing_keys set retired_at = now() where use = $1 and retired_at is null`, use); err != nil {
		return Key{}, err
	}
	if _, err := tx.Exec(ctx, `insert into signing_keys (`+cols+`) values ($1, $2, $3, $4, $5, null)`,
		k.ID, use, sealed, k.CertPEM, k.CreatedAt); err != nil {
		return Key{}, err
	}
	return k, tx.Commit(ctx)
}

func (s *Store) generate(use string) (Key, string, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return Key{}, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	if err != nil {
		return Key{}, "", err
	}
	now := time.Now().UTC().Truncate(time.Second)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Keystone " + use},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return Key{}, "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return Key{}, "", err
	}
	sealed, err := crypto.Seal(s.dataKey, x509.MarshalPKCS1PrivateKey(priv))
	if err != nil {
		return Key{}, "", err
	}
	pub, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return Key{}, "", err
	}
	sum := sha256.Sum256(pub)
	return Key{
		ID:        base64.RawURLEncoding.EncodeToString(sum[:])[:16],
		Use:       use,
		Signer:    priv,
		Cert:      cert,
		CertPEM:   string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		CreatedAt: now,
	}, sealed, nil
}

// Published returns the keys relying parties should accept, active first:
// for OIDC every key retired less than 24 h ago, for SAML and LDAP the most
// recently retired one (metadata consumers refresh less predictably).
func (s *Store) Published(ctx context.Context, use string) ([]Key, error) {
	active, err := s.Active(ctx, use)
	if err != nil {
		return nil, err
	}
	q := `select ` + cols + ` from signing_keys where use = $1 and retired_at is not null order by retired_at desc limit 1`
	args := []any{use}
	if use == OIDC {
		q = `select ` + cols + ` from signing_keys where use = $1 and retired_at > $2 order by retired_at desc`
		args = append(args, time.Now().Add(-oidcOverlap))
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Key{active}
	for rows.Next() {
		k, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
