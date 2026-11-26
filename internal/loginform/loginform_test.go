package loginform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func must(t *testing.T, page, base string) *Form {
	t.Helper()
	u, _ := url.Parse(base)
	f, err := Find([]byte(page), u)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	return f
}

func TestFindClassicForm(t *testing.T) {
	t.Parallel()
	f := must(t, `<html><body>
<form action="/search"><input name="q"></form>
<form id="login" method="post" action="do_login.php?x=1#top">
  <input type="hidden" name="csrf_token" value="abc">
  <input type="text" name="lang" value="nl">
  <label>User <input type="email" name="j_username"></label>
  <input type="password" name="j_password">
  <input type="checkbox" name="remember" checked>
  <input type="checkbox" name="news">
  <select name="realm"><option>local</option><option value="ad" selected>AD</option></select>
  <button type="submit" name="login" value="1">Log in</button>
  <button type="submit" name="other" value="2">Other</button>
</form></body></html>`, "http://nas.home.lan/admin/index.html")
	want := &Form{
		Action: "http://nas.home.lan/admin/do_login.php?x=1", Method: "POST", UserField: "j_username", PassField: "j_password",
		Extra: []Field{{"csrf_token", "abc"}, {"lang", "nl"}, {"remember", "on"}, {"realm", "ad"}, {"login", "1"}},
		Token: true,
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("got  %+v\nwant %+v", f, want)
	}
}

func TestFindPicksLoginOverSignup(t *testing.T) {
	t.Parallel()
	f := must(t, `<form action="/register" method="post"><input name="u"><input type="password" name="p1"><input type="password" name="p2"></form>
<form method="post"><input name="user"><input type="password" name="pass"></form>`, "https://app.home.lan/login")
	if f.Action != "https://app.home.lan/login" || f.UserField != "user" || f.PassField != "pass" {
		t.Fatalf("%+v", f)
	}
}

func TestFindPrefersNamedUserField(t *testing.T) {
	t.Parallel()
	f := must(t, `<form method=post action=/auth><input name=Username><input name=domain value=WORKGROUP><input type=password name=Password></form>`, "http://r.lan/")
	if f.UserField != "Username" || len(f.Extra) != 1 || f.Extra[0] != (Field{"domain", "WORKGROUP"}) {
		t.Fatalf("%+v", f)
	}
}

func TestFindPasswordOnly(t *testing.T) {
	t.Parallel()
	f := must(t, `<base href="/cgi-bin/"><form method=post action=luci><input type=password name=luci_password></form>`, "http://192.168.1.1/")
	if f.UserField != "" || f.Action != "http://192.168.1.1/cgi-bin/luci" {
		t.Fatalf("%+v", f)
	}
}

func TestFindFieldsOutsideForm(t *testing.T) {
	t.Parallel()
	f := must(t, `<form id=f method=post action=/l></form><input form=f name=u><input form=f type=password name=p>`, "http://x.lan/")
	if f.UserField != "u" || f.PassField != "p" {
		t.Fatalf("%+v", f)
	}
}

func TestFindNoForm(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("http://x.lan/")
	for _, page := range []string{
		`<div id="root"></div><script type="module" src="/a.js"></script>`,
		`<form><input type=password></form>`, // no name: JavaScript sends it
		`<form><input name=q></form>`,
	} {
		if _, err := Find([]byte(page), u); !errors.Is(err, ErrNoForm) {
			t.Fatalf("%q: err = %v", page, err)
		}
	}
	if !scripted([]byte(`<!doctype html><html><head><script type="module" src="/x.js"></script></head><body><div id="app"></div></body></html>`)) {
		t.Fatal("single-page app not seen")
	}
}

func TestFetchFollowsLoginLink(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1"})
		_, _ = w.Write([]byte(`<a href="/about">About</a> <a href="/user/login?next=/">Sign in</a>`))
	})
	mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("sid"); err != nil {
			t.Error("cookie from the first page not sent")
		}
		_, _ = w.Write([]byte(`<form method=post><input name=name><input type=password name=pass><input type=hidden name=form_build_id value=xyz></form>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p, err := Fetch(context.Background(), NewClient(5*time.Second), srv.URL+"/")
	if err != nil || p.Form == nil {
		t.Fatalf("p = %+v, err = %v", p, err)
	}
	if p.Form.Action != srv.URL+"/user/login?next=/" || p.Form.UserField != "name" || len(p.Cookies) != 1 {
		t.Fatalf("%+v", p.Form)
	}
}

func TestFetchRedirectToLogin(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ui/login", http.StatusFound) })
	mux.HandleFunc("/ui/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<form method=post action=/api/login><input name=user><input type=password name=pw></form>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p, err := Fetch(context.Background(), NewClient(5*time.Second), srv.URL)
	if err != nil || p.Form == nil || p.URL != srv.URL+"/ui/login" || p.Form.Action != srv.URL+"/api/login" {
		t.Fatalf("p = %+v, err = %v", p, err)
	}
}
