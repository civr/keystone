package httpapi

import "github.com/go-chi/chi/v5"

// adminRoutes mounts /api/admin. Everything needs a finished setup and a
// session; writes additionally need a recent confirmation (step-up). Each
// account manages its own sessions and passkeys; the rest is for admins, and
// recovery codes, the emergency account and AD are the owner's.
func (s *server) adminRoutes(r chi.Router) {
	// Gates are applied per route (not r.Use) so unknown paths still answer 404.
	me := r.With(s.requireSetupDone, requireSession)
	meW := me.With(requireStepUp)
	g := r.With(s.requireSetupDone, s.requireAdmin)
	w := g.With(requireStepUp)
	own := r.With(s.requireSetupDone, requireOwner)
	ownW := own.With(requireStepUp)

	g.Get("/settings", s.getSettings)
	w.Put("/settings", s.putSettings)

	g.Get("/branding", s.getBranding)
	w.Put("/branding", s.putBranding)

	g.Get("/users", s.listUsers)
	w.Post("/users", s.createUser)
	w.Put("/users/{id}", s.updateUser)
	w.Delete("/users/{id}", s.deleteUser)

	own.Get("/ad", s.getAD)
	ownW.Put("/ad", s.putAD)
	ownW.Post("/ad/test", s.testAD)

	me.Get("/sessions", s.listSessions)
	meW.Delete("/sessions/{id}", s.deleteSession)

	me.Get("/passkeys", s.listPasskeys)
	meW.Post("/passkeys/begin", s.passkeyAddBegin)
	meW.Post("/passkeys/finish", s.passkeyAddFinish)
	meW.Patch("/passkeys/{id}", s.renamePasskey)
	meW.Delete("/passkeys/{id}", s.deletePasskey)

	ownW.Post("/recovery/regenerate", s.regenerateRecovery)
	own.Get("/emergency", s.getEmergency)
	ownW.Put("/emergency", s.putEmergency)

	g.Get("/keys", s.listKeys)
	w.Post("/keys/{use}/rotate", s.rotateKey)

	g.Get("/audit", s.listAudit)
	g.Get("/audit/export", s.exportAudit)
}

// accountRoutes mounts /api/account: the signed-in account's own password.
func (s *server) accountRoutes(r chi.Router) {
	r.With(s.requireSetupDone, requireStepUp).Put("/password", s.changeOwnPassword)
}

// appRoutes mounts /api/apps: everyone signed in sees the apps they may use
// and opens vault tiles; admins change apps (with a step-up).
func (s *server) appRoutes(r chi.Router) {
	// Gates are applied per route (not r.Use) so unknown paths still answer 404.
	me := r.With(s.requireSetupDone, requireSession)
	g := r.With(s.requireSetupDone, s.requireAdmin)
	w := g.With(requireStepUp)

	me.Get("/", s.listApps)
	w.Post("/", s.createApp)
	w.Put("/order", s.reorderApps)
	w.Post("/icon", s.fetchAppIcon)
	w.Put("/{id}", s.updateApp)
	w.Delete("/{id}", s.deleteApp)
	w.Post("/{id}/secret", s.appSecret)
	me.Get("/{id}/vault", s.getVault)
	w.Put("/{id}/vault", s.putVault)
	me.Post("/{id}/vault/password", s.vaultPassword)
	me.Post("/{id}/vault/launch", s.vaultLaunch)
}
