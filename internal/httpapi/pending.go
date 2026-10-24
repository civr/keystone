package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/civr/keystone/internal/settings"
)

// PendingCookie carries short-lived, not-yet-authenticated state (setup
// unlock now, login steps later). Its value is "<kind>.<token>" so the kinds
// cannot be confused.
const PendingCookie = "ks_pending"

// randomToken returns 32 random bytes, base64url encoded.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// setCookie writes an HttpOnly cookie with SameSite from settings, Secure when
// r came in over https (cookieSecure). maxAge <= 0 deletes the cookie.
func setCookie(w http.ResponseWriter, r *http.Request, cfg settings.Settings, name, value string, maxAge time.Duration) {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(r, cfg),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge / time.Second),
	}
	if cfg.Session.SameSite == "Strict" {
		c.SameSite = http.SameSiteStrictMode
	}
	if maxAge <= 0 {
		c.Value, c.MaxAge = "", -1
	}
	http.SetCookie(w, c)
}

// cookieSecure reports whether r reached Keystone over https: by its Origin
// when the browser sent one, else by arriving on an https public_url's host.
// On another address (the server's IP over http) a Secure cookie would be
// dropped and the sign-in would never stick.
func cookieSecure(r *http.Request, cfg settings.Settings) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return strings.HasPrefix(strings.ToLower(o), "https://")
	}
	p, err := url.Parse(cfg.PublicURL)
	return err == nil && strings.EqualFold(p.Scheme, "https") && normHost("https", r.Host) == normHost("https", p.Host)
}

// pendingToken returns the token of the ks_pending cookie if it has kind.
func pendingToken(r *http.Request, kind string) (string, bool) {
	c, err := r.Cookie(PendingCookie)
	if err != nil {
		return "", false
	}
	tok, ok := strings.CutPrefix(c.Value, kind+".")
	return tok, ok && tok != ""
}
