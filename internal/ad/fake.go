package ad

import (
	"context"
	"time"
)

// FakeUser is an in-memory account for tests.
type FakeUser struct {
	Password, DisplayName, GivenName, Email string
}

// Fake is an in-memory Directory. Users is keyed by normalized username.
type Fake struct {
	Users map[string]FakeUser
	Down  bool
	Delay time.Duration
}

func (f *Fake) wait(ctx context.Context) error {
	if f.Down {
		return ErrUnavailable
	}
	if f.Delay > 0 {
		t := time.NewTimer(f.Delay)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return ErrUnavailable
		}
	}
	return nil
}

func (f *Fake) Lookup(ctx context.Context, username string) (User, error) {
	if err := f.wait(ctx); err != nil {
		return User{}, err
	}
	n := NormalizeUsername(username)
	u, ok := f.Users[n]
	if !ok {
		return User{}, ErrNotFound
	}
	return User{Username: n, DisplayName: u.DisplayName, GivenName: u.GivenName, Email: u.Email, DN: "cn=" + n + ",dc=fake"}, nil
}

func (f *Fake) Authenticate(ctx context.Context, username, password string) (User, error) {
	u, err := f.Lookup(ctx, username)
	if err != nil {
		return User{}, err
	}
	if password == "" || f.Users[u.Username].Password != password {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}
