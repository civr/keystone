package httpapi

import "github.com/go-chi/chi/v5"

// adminRoutes mounts /api/admin. Everything needs a finished setup and a
// session; writes additionally need a recent passkey (step-up).
func (s *server) adminRoutes(r chi.Router) {
	// Gates are applied per route (not r.Use) so unknown paths still answer 404.
	g := r.With(s.requireSetupDone, requireSession)
	w := g.With(requireStepUp)

	g.Get("/settings", s.getSettings)
	w.Put("/settings", s.putSettings)

	g.Get("/ad", s.getAD)
	w.Put("/ad", s.putAD)
	w.Post("/ad/test", s.testAD)

	g.Get("/sessions", s.listSessions)
	w.Delete("/sessions/{id}", s.deleteSession)

	g.Get("/passkeys", s.listPasskeys)
	w.Post("/passkeys/begin", s.passkeyAddBegin)
	w.Post("/passkeys/finish", s.passkeyAddFinish)
	w.Patch("/passkeys/{id}", s.renamePasskey)
	w.Delete("/passkeys/{id}", s.deletePasskey)

	w.Post("/recovery/regenerate", s.regenerateRecovery)
	g.Get("/emergency", s.getEmergency)
	w.Put("/emergency", s.putEmergency)

	g.Get("/keys", s.listKeys)
	w.Post("/keys/{use}/rotate", s.rotateKey)

	g.Get("/audit", s.listAudit)
	g.Get("/audit/export", s.exportAudit)
}

// appRoutes mounts /api/apps: reading needs a session, writing a step-up.
func (s *server) appRoutes(r chi.Router) {
	// Gates are applied per route (not r.Use) so unknown paths still answer 404.
	g := r.With(s.requireSetupDone, requireSession)
	w := g.With(requireStepUp)

	g.Get("/", s.listApps)
	w.Post("/", s.createApp)
	w.Put("/order", s.reorderApps)
	w.Put("/{id}", s.updateApp)
	w.Delete("/{id}", s.deleteApp)
	w.Post("/{id}/secret", s.appSecret)
	g.Get("/{id}/vault", s.getVault)
	w.Put("/{id}/vault", s.putVault)
	g.Post("/{id}/vault/password", s.vaultPassword)
}
