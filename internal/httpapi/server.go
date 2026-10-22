// Package httpapi is Keystone core's HTTP layer: router, security middleware,
// JSON API handlers and the SPA shell.
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/keys"
	"github.com/civr/keystone/internal/secrets"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

// Deps are the services the HTTP layer uses.
type Deps struct {
	Pool     *pgxpool.Pool
	Settings *settings.Store
	Valkey   *redis.Client // pending-login and WebAuthn ceremony state
	Sessions *session.Store
	Limiter  *session.Limiter
	Dir      func() ad.Directory // current AD directory; nil while unconfigured
	Audit    *audit.Writer
	Secrets  secrets.Secrets
	Shell    []byte // SPA shell; EmbeddedShell when empty
	Keys     *keys.Store // signing keys; keys.New(Pool, Secrets.DataKey) when nil

	// SetupCodeHash is the argon2 hash of the setup code printed at startup;
	// empty once setup is complete.
	SetupCodeHash string
	// TestAD checks an AD configuration (connect, service bind, count users).
	// Nil means ad.Test with Secrets.DataKey; tests substitute a fake.
	TestAD func(ctx context.Context, cfg ad.Config) (int, error)
}

type server struct {
	d     Deps
	keys  *keys.Store
	shell []byte
	csp   string // policy for shell, without settings headers.csp_extra
}

func newServer(d Deps) *server {
	shell := d.Shell
	if len(shell) == 0 {
		shell = EmbeddedShell
	}
	k := d.Keys
	if k == nil {
		k = keys.New(d.Pool, d.Secrets.DataKey)
	}
	return &server{d: d, keys: k, shell: shell, csp: CSP(shell, "")}
}

// New returns the core HTTP handler.
func New(d Deps) http.Handler { return newServer(d).router() }

func (s *server) router() chi.Router {
	r := chi.NewRouter()
	r.Use(s.middlewares()...)
	r.Route("/api", func(api chi.Router) {
		api.NotFound(errNotFound)
		api.MethodNotAllowed(errMethodNotAllowed)
		api.Get("/healthz", s.healthz)
		api.Route("/setup", s.setupRoutes)
		api.Route("/auth", s.authRoutes)
		api.With(requireSession).Get("/me", s.me)
		api.Route("/admin", s.adminRoutes)
		api.Route("/apps", s.appRoutes)
		api.With(s.requireSetupDone, requireSessionOrLogin).Get("/gate/start", s.gateStart)
	})
	r.Route("/saml", s.samlRoutes)
	r.Route("/oidc", s.oidcRoutes)
	r.With(s.requireSetupDone).Get("/.well-known/openid-configuration", s.oidcDiscovery)
	r.NotFound(s.serveShell)
	r.MethodNotAllowed(errMethodNotAllowed)
	return r
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.d.Pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
