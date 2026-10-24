// Package accounts is everyone who can sign in to Keystone: the owner, chosen
// during setup (table owner: AD link, recovery codes, the emergency account),
// and the accounts the admins add (table users), each with a Keystone
// password. The owner is always an admin.
package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// OwnerID is the owner's account id; users have uuids.
const OwnerID = "owner"

const (
	Admin  = "admin"
	Member = "member"
)

// Account is one person who can sign in.
type Account struct {
	ID          string
	Username    string
	DisplayName string
	GivenName   string
	Email       string
	UserHandle  []byte
	// PasswordHash is the Keystone password (argon2); empty for an owner who
	// signs in with AD.
	PasswordHash string
	Role         string
	Disabled     bool
	CreatedAt    time.Time
}

// IsOwner reports whether a is the owner.
func (a Account) IsOwner() bool { return a.ID == OwnerID }

// IsAdmin reports whether a may change settings, apps and accounts.
func (a Account) IsAdmin() bool { return a.IsOwner() || a.Role == Admin }

// Name is the display name, or the username without one.
func (a Account) Name() string {
	if strings.TrimSpace(a.DisplayName) != "" {
		return a.DisplayName
	}
	return a.Username
}

var (
	// ErrNotFound means no such account.
	ErrNotFound = errors.New("accounts: not found")
	// ErrTaken means the username is in use.
	ErrTaken = errors.New("accounts: username taken")
)

// Reader is satisfied by *pgxpool.Pool and pgx.Tx.
type Reader interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Querier is a Reader that can also write.
type Querier interface {
	Reader
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidUsername reports whether u (already lowercased) can be a username.
func ValidUsername(u string) bool { return usernameRe.MatchString(u) }

const ownerSelect = `select 'owner', username, coalesce(display_name, ''), coalesce(given_name, ''), coalesce(email, ''),
	user_handle, coalesce(password_hash, ''), 'admin', false, created_at from owner where id = 1`

const userSelect = `select id::text, username, display_name, '', email, user_handle, password_hash, role,
	disabled_at is not null, created_at from users`

func scan(row pgx.Row) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.Username, &a.DisplayName, &a.GivenName, &a.Email, &a.UserHandle, &a.PasswordHash, &a.Role, &a.Disabled, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// Load returns the account with id.
func Load(ctx context.Context, q Reader, id string) (Account, error) {
	if id == OwnerID || id == "" {
		return scan(q.QueryRow(ctx, ownerSelect))
	}
	return scan(q.QueryRow(ctx, userSelect+` where id::text = $1`, id))
}

// Owner returns the owner.
func Owner(ctx context.Context, q Reader) (Account, error) { return Load(ctx, q, OwnerID) }

// ByUsername returns the account signing in as username (lowercase).
func ByUsername(ctx context.Context, q Reader, username string) (Account, error) {
	a, err := scan(q.QueryRow(ctx, ownerSelect+` and username = $1`, username))
	if !errors.Is(err, ErrNotFound) {
		return a, err
	}
	return scan(q.QueryRow(ctx, userSelect+` where username = $1`, username))
}

// ByHandle returns the account whose WebAuthn user handle is h.
func ByHandle(ctx context.Context, q Reader, h []byte) (Account, error) {
	a, err := scan(q.QueryRow(ctx, ownerSelect+` and user_handle = $1`, h))
	if !errors.Is(err, ErrNotFound) {
		return a, err
	}
	return scan(q.QueryRow(ctx, userSelect+` where user_handle = $1`, h))
}

// List returns the owner (when set up) and then every user by username.
func List(ctx context.Context, q Reader) ([]Account, error) {
	var out []Account
	o, err := Owner(ctx, q)
	switch {
	case err == nil:
		out = append(out, o)
	case !errors.Is(err, ErrNotFound):
		return nil, err
	}
	rows, err := q.Query(ctx, userSelect+` order by username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// New is a user to create.
type New struct {
	Username, DisplayName, Email, PasswordHash, Role string
}

// Create adds a user. The username must not be the owner's, nor one a
// deleted account had.
func Create(ctx context.Context, q Querier, n New) (Account, error) {
	var retired bool
	if err := q.QueryRow(ctx, `select exists(select 1 from retired_usernames where username = $1)`, n.Username).Scan(&retired); err != nil {
		return Account{}, err
	}
	if retired {
		return Account{}, ErrTaken
	}
	if taken, err := ownerHas(ctx, q, n.Username); err != nil || taken {
		if err == nil {
			err = ErrTaken
		}
		return Account{}, err
	}
	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		return Account{}, err
	}
	a, err := scan(q.QueryRow(ctx, `insert into users (username, display_name, email, user_handle, password_hash, role)
		values ($1, $2, $3, $4, $5, $6) returning id::text, username, display_name, '', email, user_handle, password_hash, role,
		disabled_at is not null, created_at`, n.Username, n.DisplayName, n.Email, handle, n.PasswordHash, n.Role))
	if isUnique(err) {
		return Account{}, ErrTaken
	}
	return a, err
}

// Change is an edit of a user; nil fields stay as they are.
type Change struct {
	DisplayName, Email, Role, PasswordHash *string
	Disabled                               *bool
}

// Update edits user id (not the owner).
func Update(ctx context.Context, q Querier, id string, c Change) (Account, error) {
	if id == OwnerID {
		return Account{}, fmt.Errorf("accounts: the owner is edited in setup and Settings")
	}
	return scan(q.QueryRow(ctx, `update users set
		display_name = coalesce($2, display_name),
		email = coalesce($3, email),
		role = coalesce($4, role),
		password_hash = coalesce($5, password_hash),
		disabled_at = case when $6::bool is null then disabled_at when $6 then coalesce(disabled_at, now()) else null end
		where id::text = $1
		returning id::text, username, display_name, '', email, user_handle, password_hash, role, disabled_at is not null, created_at`,
		id, c.DisplayName, c.Email, c.Role, c.PasswordHash, c.Disabled))
}

// SetPassword stores a new password hash for any account, the owner included.
func SetPassword(ctx context.Context, q Querier, id, hash string) error {
	var tag pgconn.CommandTag
	var err error
	if id == OwnerID {
		tag, err = q.Exec(ctx, `update owner set password_hash = $1 where id = 1`, hash)
	} else {
		tag, err = q.Exec(ctx, `update users set password_hash = $2 where id::text = $1`, id, hash)
	}
	if err == nil && tag.RowsAffected() != 1 {
		err = ErrNotFound
	}
	return err
}

// Delete removes user id with its passkeys and vault items.
func Delete(ctx context.Context, q Querier, id string) error {
	if id == OwnerID {
		return fmt.Errorf("accounts: the owner cannot be deleted")
	}
	var n int
	err := q.QueryRow(ctx, `with gone as (delete from users where id::text = $1 returning username),
		kept as (insert into retired_usernames (username) select username from gone on conflict do nothing)
		select count(*) from gone`, id).Scan(&n)
	if err == nil && n != 1 {
		err = ErrNotFound
	}
	return err
}

// EmailTaken reports whether email (case-insensitive) belongs to an account
// other than except.
func EmailTaken(ctx context.Context, q Reader, email, except string) (bool, error) {
	list, err := List(ctx, q)
	if err != nil {
		return false, err
	}
	for _, a := range list {
		if a.ID != except && a.Email != "" && strings.EqualFold(a.Email, email) {
			return true, nil
		}
	}
	return false, nil
}

// Admins counts the admins that can sign in: the owner and enabled admin users.
func Admins(ctx context.Context, q Reader) (int, error) {
	var n int
	err := q.QueryRow(ctx, `select (select count(*) from owner) + (select count(*) from users where role = 'admin' and disabled_at is null)`).Scan(&n)
	return n, err
}

func ownerHas(ctx context.Context, q Reader, username string) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `select count(*) from owner where username = $1`, username).Scan(&n)
	return n > 0, err
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// Ref is how account columns point at an account: nil for the owner.
func Ref(id string) *string {
	if id == OwnerID || id == "" {
		return nil
	}
	return &id
}
