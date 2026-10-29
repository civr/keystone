package onepassword

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1/vaults/v1/items/i1":
			_, _ = w.Write([]byte(`{"fields":[{"purpose":"USERNAME","value":"admin"},{"purpose":"PASSWORD","value":"hunter2"},{"purpose":"NOTES","value":"rack 2"}],"urls":[{"href":"https://nas.lan","primary":true}]}`))
		case "/v1/vaults/v1/items/i2":
			_, _ = w.Write([]byte(`{"fields":[{"type":"CONCEALED","label":"credential","value":"sk-123"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Token: "tok"}
	s, err := c.Get(context.Background(), "v1", "i1")
	if err != nil || s.Username != "admin" || s.Password != "hunter2" || s.Note != "rack 2" || s.URL != "https://nas.lan" {
		t.Fatalf("login = %+v %v", s, err)
	}
	if s, err := c.Get(context.Background(), "v1", "i2"); err != nil || s.Password != "sk-123" {
		t.Fatalf("api key = %+v %v", s, err)
	}
	if _, err := c.Get(context.Background(), "v1", "nope"); err != ErrNotFound {
		t.Fatalf("missing = %v", err)
	}
	if _, err := (&Client{URL: srv.URL, Token: "bad"}).Vaults(context.Background()); err == nil {
		t.Fatal("bad token accepted")
	}
}
