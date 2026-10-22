package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireSessionOrLoginRedirects(t *testing.T) {
	h := requireSessionOrLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/oidc/authorize?client_id=x", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login?next=%2Foidc%2Fauthorize%3Fclient_id%3Dx" {
		t.Fatalf("got %d %q", w.Code, w.Header().Get("Location"))
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("redirect must not be cached")
	}
}
