package gate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/civr/keystone/internal/apps"
)

// An app with a CSRF-protected login form: the gate reads the login page
// first, sends its token and cookie back, and hands on the session cookies.
func TestFormLoginWithToken(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "csrf", Value: "c1", Path: "/", HttpOnly: true})
		_, _ = w.Write([]byte(`<form method="post" action="https://app.example.com/session?next=/">
<input type="hidden" name="authenticity_token" value="t1"><input name="login"><input type="password" name="password"><input type="submit" name="commit" value="Sign in"></form>`))
	})
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		c, err := r.Cookie("csrf")
		if err != nil || c.Value != "c1" || r.PostForm.Get("authenticity_token") != "t1" || r.PostForm.Get("commit") != "Sign in" || r.URL.Query().Get("next") != "/" {
			http.Error(w, "csrf", http.StatusUnprocessableEntity)
			return
		}
		if r.PostForm.Get("login") != "thijs" || r.PostForm.Get("password") != "pw" {
			_, _ = w.Write([]byte(`<form method="post" action="/session"><input name="login"><input type="password" name="password"></form>`))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "s1", Path: "/", HttpOnly: true})
		http.Redirect(w, r, "/", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	si := &apps.GateSignIn{Mode: apps.SignInForm, Upstream: srv.URL, LoginPath: "/login", UserField: "login", PassField: "password"}
	s := &Server{}

	status, cookies, err := s.formLogin(context.Background(), si, "thijs", "pw", true)
	if err != nil || status != http.StatusFound {
		t.Fatalf("status %d, err %v", status, err)
	}
	got := map[string]string{}
	for _, c := range cookies {
		got[c.Name] = c.Value
		if !c.Secure {
			t.Errorf("cookie %s not Secure", c.Name)
		}
	}
	if got["session"] != "s1" || got["csrf"] != "c1" {
		t.Fatalf("cookies %v", got)
	}

	if _, _, err := s.formLogin(context.Background(), si, "thijs", "wrong", true); err == nil {
		t.Fatal("a login form shown again counts as signed in")
	}
}

// An app whose login path only takes the POST still signs in as before.
func TestFormLoginWithoutPage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		if r.PostForm.Get("u") != "a" || r.PostForm.Get("p") != "b" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "x"})
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	si := &apps.GateSignIn{Mode: apps.SignInForm, Upstream: srv.URL, LoginPath: "/api/login", UserField: "u", PassField: "p"}
	if _, c, err := (&Server{}).formLogin(context.Background(), si, "a", "b", false); err != nil || len(c) != 1 || c[0].Name != "sid" {
		t.Fatalf("cookies %v, err %v", c, err)
	}
}
