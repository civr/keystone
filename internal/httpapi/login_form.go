package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/civr/keystone/internal/loginform"
)

// detectLoginForm opens an app's page and reports its login form, so the
// app editor fills in the login address and field names by itself.
func (s *server) detectLoginForm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		writeInvalid(w, "url", "The URL must be absolute and start with http:// or https://.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	p, err := loginform.Fetch(ctx, loginform.NewClient(8*time.Second), u.String())
	if err != nil {
		msg := "Keystone could not open " + u.Host + ". Check that Keystone can reach it, or fill in the fields yourself."
		if errors.Is(err, context.DeadlineExceeded) {
			msg = u.Host + " did not answer in time. Check that Keystone can reach it, or fill in the fields yourself."
		}
		writeError(w, http.StatusBadGateway, "unreachable", msg)
		return
	}
	if p.Form == nil && p.App == nil {
		msg := "No login form was found on this page. Open the app’s login page and try that address."
		if p.Script {
			msg = "This app draws its login with JavaScript and Keystone doesn’t recognise it. Use JSON sign-in with the app’s login API, or copy the password."
		}
		writeError(w, http.StatusNotFound, "no_form", msg)
		return
	}
	out := map[string]any{"page_url": p.URL, "app": p.App}
	var notes []string
	if f := p.Form; f != nil {
		extra := make([]string, 0, len(f.Extra))
		for _, e := range f.Extra {
			extra = append(extra, e.Name)
		}
		out["login_url"], out["method"], out["user_field"], out["pass_field"] = f.Action, f.Method, f.UserField, f.PassField
		out["extra"], out["token"] = extra, f.Token
		if f.Method != http.MethodPost {
			notes = append(notes, "The form sends with GET; Keystone signs in with POST, which most apps accept.")
		}
		if f.UserField == "" {
			notes = append(notes, "The form only asks for a password.")
		}
		if f.Token && len(p.Cookies) > 0 {
			notes = append(notes, "The form carries a security token tied to a cookie. A vault tile can’t send that from another site; if signing in fails, put the app behind the gate and use its Login form sign-in.")
		}
	}
	out["notes"] = notes
	writeJSON(w, http.StatusOK, out)
}
