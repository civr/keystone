package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/samlidp"
	"github.com/civr/keystone/internal/session"
)

// A SAML request that arrives before sign-in waits here while the browser
// goes through /login; the login page returns to /saml/sso?resume=<token>.
const samlStashTTL = 10 * time.Minute

func samlStashKey(tok string) string { return "samlreq:" + tok }

func (s *server) samlRoutes(r chi.Router) {
	r.Use(s.requireSetupDone)
	r.Get("/metadata", s.samlMetadata)
	r.Get("/sso", s.samlSSO)
	r.Post("/sso", s.samlSSO)
	r.With(requireSessionOrLogin).Get("/idp/{id}", s.samlIdPInitiated)
}

func (s *server) samlProvider() *samlidp.Provider {
	return &samlidp.Provider{Keys: s.keys, Apps: s.d.Pool}
}

func (s *server) samlMetadata(w http.ResponseWriter, r *http.Request) {
	md, err := s.samlProvider().Metadata(r.Context(), settingsFrom(r.Context()).PublicURL)
	if err != nil {
		errInternal(w, "saml metadata", err)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write(md)
}

// samlFail answers a request that cannot be honoured. It never redirects:
// the target of a bad request is not trusted.
func samlFail(w http.ResponseWriter, status int, msg string) {
	writeError(w, status, "saml_invalid", msg)
}

func (s *server) samlSSO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req samlidp.Request
	if tok := r.URL.Query().Get("resume"); tok != "" && r.Method == http.MethodGet {
		raw, err := s.d.Valkey.GetDel(ctx, samlStashKey(tok)).Bytes()
		if errors.Is(err, redis.Nil) {
			samlFail(w, http.StatusBadRequest, "This sign-in attempt has expired. Open the app again.")
			return
		}
		if err != nil {
			errInternal(w, "saml resume", err)
			return
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			errInternal(w, "saml resume", err)
			return
		}
	} else {
		var err error
		if req, err = samlidp.Parse(r); err != nil {
			slog.Info("saml: bad request", "err", err)
			samlFail(w, http.StatusBadRequest, "Invalid SAML request.")
			return
		}
	}
	sess := SessionFrom(ctx)
	if sess == nil || sess.MustRegisterPasskey {
		// Only a valid request from a configured app may wait in Valkey, and
		// only a few per address: the endpoint takes unauthenticated posts.
		if !s.allow(w, ctx, "saml-stash", clientIP(ctx).String(), 20) {
			return
		}
		if err := s.samlProvider().Check(ctx, settingsFrom(ctx).PublicURL, r, req); err != nil {
			slog.Info("saml: request refused before sign-in", "err", err)
			samlFail(w, http.StatusBadRequest, "This SAML request did not come from an app set up in Keystone.")
			return
		}
		tok, err := randomToken()
		if err != nil {
			errInternal(w, "saml stash", err)
			return
		}
		raw, _ := json.Marshal(req)
		if err := s.d.Valkey.Set(ctx, samlStashKey(tok), raw, samlStashTTL).Err(); err != nil {
			errInternal(w, "saml stash", err)
			return
		}
		r.URL.RawQuery = "resume=" + tok
		r.Method = http.MethodGet
		requireSessionOrLogin(nil).ServeHTTP(w, r)
		return
	}
	owner, err := s.account(ctx)
	if err != nil {
		errInternal(w, "saml account", err)
		return
	}
	f, a, err := s.samlProvider().Respond(ctx, settingsFrom(ctx).PublicURL, r, req, owner, samlLogin(sess))
	switch {
	case err == nil && lacksMFA(ctx, a):
		samlFail(w, http.StatusForbidden, msgNeedsMFA)
		return
	case err == nil && !a.AllowedFor(owner):
		samlFail(w, http.StatusForbidden, "You do not have access to this app.")
		return
	case errors.Is(err, samlidp.ErrUnknownSP):
		samlFail(w, http.StatusBadRequest, "This app is not set up as a SAML app in Keystone.")
		return
	case err != nil:
		slog.Info("saml: request refused", "err", err)
		samlFail(w, http.StatusBadRequest, "The SAML request from the app does not match its settings in Keystone.")
		return
	}
	s.appAccess(ctx, a, "saml")
	_ = samlidp.WriteForm(w, f)
}

func (s *server) samlIdPInitiated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := apps.ByID(ctx, s.d.Pool, chi.URLParam(r, "id"))
	if errors.Is(err, apps.ErrNotFound) || (err == nil && (a.Config.SAML == nil || !a.Config.SAML.IdPInitiated)) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "saml app", err)
		return
	}
	owner, err := s.account(ctx)
	if err != nil {
		errInternal(w, "saml account", err)
		return
	}
	if !a.AllowedFor(owner) {
		errNotFound(w, r)
		return
	}
	if lacksMFA(ctx, a) {
		samlFail(w, http.StatusForbidden, msgNeedsMFA)
		return
	}
	sess := SessionFrom(ctx)
	f, err := s.samlProvider().RespondIdPInitiated(ctx, settingsFrom(ctx).PublicURL, r, a, owner, samlLogin(sess))
	if err != nil {
		errInternal(w, "saml idp-initiated", err)
		return
	}
	s.appAccess(ctx, a, "saml")
	_ = samlidp.WriteForm(w, f)
}

func samlLogin(sess *session.Session) samlidp.Login {
	return samlidp.Login{SessionID: sess.ID, AuthAt: sess.LastFullAuthAt, AMR: sess.AMR()}
}

// appAccess audits a sign-in into an app.
func (s *server) appAccess(ctx context.Context, a apps.App, protocol string) {
	s.d.Audit.Log(ctx, actor(ctx), "app.access", clientIP(ctx), map[string]any{"app": a.Name, "id": a.ID, "protocol": protocol})
}
