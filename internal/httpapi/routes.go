package httpapi

import (
	"github.com/go-chi/chi/v5"

	"github.com/civr/keystone/internal/rbac"
)

// adminRoutes mounts /api/admin. Everything needs a finished setup and a
// session; writes additionally need a recent confirmation (step-up). Each
// route names the permissions that open it (package rbac); each account
// manages its own sessions and passkeys, and recovery codes, the emergency
// account and AD are the owner's.
func (s *server) adminRoutes(r chi.Router) {
	// Gates are applied per route (not r.Use) so unknown paths still answer 404.
	done := r.With(s.requireSetupDone)
	me := done.With(requireSession)
	meW := me.With(requireStepUp)
	can := func(perms ...string) chi.Router { return done.With(s.need(perms...)) }
	canW := func(perms ...string) chi.Router { return done.With(s.need(perms...), requireStepUp) }
	own := done.With(requireOwner)
	ownW := own.With(requireStepUp)

	can(rbac.SecurityWrite, rbac.SystemWrite, rbac.AuthWrite).Get("/settings", s.getSettings)
	canW(rbac.SecurityWrite, rbac.SystemWrite).Put("/settings", s.putSettings)

	can().Get("/branding", s.getBranding)
	canW(rbac.BrandingWrite).Put("/branding", s.putBranding)

	can(rbac.UsersRead, rbac.UsersWrite).Get("/users", s.listUsers)
	canW(rbac.UsersWrite).Post("/users", s.createUser)
	canW(rbac.UsersWrite).Put("/users/{id}", s.updateUser)
	canW(rbac.UsersWrite).Delete("/users/{id}", s.deleteUser)
	can(rbac.UsersRead, rbac.RolesWrite).Get("/users/{id}/roles", s.getUserRoles)
	canW(rbac.RolesWrite).Put("/users/{id}/roles", s.putUserRoles)

	can(rbac.UsersRead, rbac.RolesWrite).Get("/roles", s.listRoles)
	canW(rbac.RolesWrite).Post("/roles", s.createRole)
	canW(rbac.RolesWrite).Put("/roles/{id}", s.updateRole)
	canW(rbac.RolesWrite).Delete("/roles/{id}", s.deleteRole)

	can(rbac.UsersRead, rbac.GroupsWrite).Get("/groups", s.listGroups)
	canW(rbac.GroupsWrite).Post("/groups", s.createGroup)
	canW(rbac.GroupsWrite).Put("/groups/{id}", s.updateGroup)
	canW(rbac.GroupsWrite).Delete("/groups/{id}", s.deleteGroup)
	canW(rbac.RolesWrite).Put("/groups/{id}/roles", s.putGroupRoles)

	own.Get("/ad", s.getAD)
	ownW.Put("/ad", s.putAD)
	ownW.Post("/ad/test", s.testAD)

	can(rbac.SessionsManage).Get("/sessions/all", s.listAllSessions)
	canW(rbac.SessionsManage).Delete("/sessions/all/{id}", s.endAnySession)
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

	can(rbac.SecurityWrite).Get("/keys", s.listKeys)
	canW(rbac.SecurityWrite).Post("/keys/{use}/rotate", s.rotateKey)

	can(rbac.AuditRead).Get("/audit", s.listAudit)
	can(rbac.AuditRead).Get("/audit/export", s.exportAudit)
	can().Get("/overview", s.overview)
}

// accountRoutes mounts /api/account: the signed-in account's own password
// and what it has access to.
func (s *server) accountRoutes(r chi.Router) {
	r.With(s.requireSetupDone, requireStepUp).Put("/password", s.changeOwnPassword)
	r.With(s.requireSetupDone, requireSession).Get("/access", s.myAccess)
}

// appRoutes mounts /api/apps: everyone signed in sees the apps they may use
// and opens vault tiles; app admins change the apps they are given, admins
// with apps.write any app (with a step-up).
func (s *server) appRoutes(r chi.Router) {
	// Gates are applied per route (not r.Use) so unknown paths still answer 404.
	me := r.With(s.requireSetupDone, requireSession)
	meW := me.With(requireStepUp)
	all := r.With(s.requireSetupDone, s.need(rbac.AppsWrite), requireStepUp)

	me.Get("/", s.listApps)
	me.Get("/recent", s.recentApps)
	me.Post("/{id}/opened", s.appOpened)
	all.Post("/", s.createApp)
	all.Put("/order", s.reorderApps)
	all.Post("/icon", s.fetchAppIcon)
	meW.Put("/{id}", s.appAdmin(s.updateApp))
	all.Delete("/{id}", s.deleteApp)
	meW.Post("/{id}/secret", s.appAdmin(s.appSecret))
	me.Get("/{id}/vault", s.getVault)
	meW.Put("/{id}/vault", s.appAdmin(s.putVault))
	me.Post("/{id}/vault/password", s.vaultPassword)
	me.Post("/{id}/vault/launch", s.vaultLaunch)
}
