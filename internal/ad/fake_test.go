package ad

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFake(t *testing.T) {
	f := &Fake{Users: map[string]FakeUser{"thijs": {Password: "pw", GivenName: "Thijs", DisplayName: "Thijs W"}}}
	ctx := context.Background()
	u, err := f.Authenticate(ctx, `HOME\THIJS`, "pw")
	if err != nil || u.Username != "thijs" || u.GivenName != "Thijs" {
		t.Fatalf("got %+v %v", u, err)
	}
	if _, err := f.Authenticate(ctx, "thijs", "bad"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
	if _, err := f.Lookup(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	f.Down = true
	if _, err := f.Lookup(ctx, "thijs"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	f.Down, f.Delay = false, time.Second
	c, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := f.Authenticate(c, "thijs", "pw"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable on ctx expiry, got %v", err)
	}
}
