package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunHealthcheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		ok     bool
	}{{"ok", 200, true}, {"unavailable", 503, false}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/healthz" {
					t.Errorf("path %q", r.URL.Path)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			err := runHealthcheck(context.Background(), srv.URL+"/api/healthz")
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/api/healthz"
	srv.Close()
	if err := runHealthcheck(context.Background(), url); err == nil {
		t.Fatal("expected error for unreachable server")
	}
}
