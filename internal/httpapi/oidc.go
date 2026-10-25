package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/oidc"
)

// issuer is the OIDC issuer: public_url without a trailing slash.
func issuer(r *http.Request) string {
	return strings.TrimRight(settingsFrom(r.Context()).PublicURL, "/")
}

func (s *server) oidcProvider() *oidc.Provider {
	return &oidc.Provider{Keys: s.keys, Valkey: s.d.Valkey, Sessions: s.d.Sessions,
		Account: func(ctx context.Context, userID string) (apps.Owner, error) {
			return accounts.Load(ctx, s.d.Pool, userID)
		}}
}

func (s *server) oidcRoutes(r chi.Router) {
	r.Use(s.requireSetupDone)
	r.With(requireSessionOrLogin).Get("/authorize", s.oidcAuthorize)
	r.Post("/token", s.oidcToken)
	r.Get("/userinfo", s.oidcUserinfo)
	r.Post("/userinfo", s.oidcUserinfo)
	r.Get("/jwks", s.oidcJWKS)
}

func (s *server) oidcDiscovery(w http.ResponseWriter, r *http.Request) {
	if issuer(r) == "" {
		errNotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, oidc.Discovery(issuer(r)))
}

func (s *server) oidcJWKS(w http.ResponseWriter, r *http.Request) {
	set, err := s.oidcProvider().JWKS(r.Context())
	if err != nil {
		errInternal(w, "oidc jwks", err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

func (s *server) oidcAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := apps.Load(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "oidc apps", err)
		return
	}
	ar, oerr := oidc.ParseAuthorize(r.URL.Query(), list)
	if oerr != nil && !oerr.Redirect {
		// Never redirect to an unverified redirect_uri.
		writeError(w, http.StatusBadRequest, oerr.Code, oerr.Description)
		return
	}
	back := func(v url.Values) {
		if ar.State != "" {
			v.Set("state", ar.State)
		}
		v.Set("iss", issuer(r))
		sep := "?"
		if strings.Contains(ar.RedirectURI, "?") {
			sep = "&"
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, ar.RedirectURI+sep+v.Encode(), http.StatusFound)
	}
	if oerr != nil {
		back(url.Values{"error": {oerr.Code}, "error_description": {oerr.Description}})
		return
	}
	if !s.mayUse(ctx, ar.App) {
		back(url.Values{"error": {"access_denied"}, "error_description": {"You do not have access to this app."}})
		return
	}
	code, err := s.oidcProvider().IssueCode(ctx, ar, SessionFrom(ctx).ID, randomToken)
	if err != nil {
		errInternal(w, "oidc code", err)
		return
	}
	s.appAccess(ctx, ar.App, "oidc")
	back(url.Values{"code": {code}})
}

// oauthError writes an RFC 6749 §5.2 error.
func oauthError(w http.ResponseWriter, status int, code, desc string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="keystone"`)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// oidcClient authenticates the client (HTTP Basic or form) and returns its app.
func (s *server) oidcClient(r *http.Request, list []apps.App) (apps.App, bool) {
	id, secret, ok := r.BasicAuth()
	if ok {
		var e1, e2 error
		id, e1 = url.QueryUnescape(id)
		secret, e2 = url.QueryUnescape(secret)
		if e1 != nil || e2 != nil {
			return apps.App{}, false
		}
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	a, found := oidc.ClientByID(list, id)
	if !found || a.Config.OIDC.SecretHash == "" || secret == "" || !crypto.VerifySecret(a.Config.OIDC.SecretHash, secret) {
		return apps.App{}, false
	}
	return a, true
}

func (s *server) oidcToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Invalid form.")
		return
	}
	if !s.allow(w, ctx, "oidc-token", clientIP(ctx).String(), 60) {
		return
	}
	list, err := apps.Load(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "oidc apps", err)
		return
	}
	client, ok := s.oidcClient(r, list)
	if !ok {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
		return
	}
	cfg, f := settingsFrom(ctx), r.PostForm
	var t oidc.Tokens
	switch f.Get("grant_type") {
	case "authorization_code":
		t, err = s.oidcProvider().Exchange(ctx, cfg, issuer(r), client, f.Get("code"), f.Get("redirect_uri"), f.Get("code_verifier"), randomToken)
	case "refresh_token":
		t, err = s.oidcProvider().Refresh(ctx, cfg, issuer(r), client, f.Get("refresh_token"), randomToken)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "Unknown grant_type.")
		return
	}
	var oe *oidc.Error
	if errors.As(err, &oe) {
		oauthError(w, http.StatusBadRequest, oe.Code, oe.Description)
		return
	}
	if err != nil {
		errInternal(w, "oidc token", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, t)
}

func (s *server) oidcUserinfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	list, err := apps.Load(ctx, s.d.Pool)
	if err != nil {
		errInternal(w, "oidc apps", err)
		return
	}
	claims, err := s.oidcProvider().Userinfo(ctx, settingsFrom(ctx), issuer(r), strings.TrimSpace(bearer), list)
	var oe *oidc.Error
	if errors.As(err, &oe) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": oe.Code})
		return
	}
	if err != nil {
		slog.Error("oidc userinfo", "err", err)
		errUnavailable(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, claims)
}
