// Package rbac is Keystone's role-based access: permissions, the roles that
// bundle them, and what an account may do — globally or for one app.
package rbac

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
)

// Permissions. All is every one of them; Super Admin holds "*".
const (
	UsersRead      = "users.read"
	UsersWrite     = "users.write"
	GroupsWrite    = "groups.write"
	RolesWrite     = "roles.write"
	AppsRead       = "apps.read"
	AppsWrite      = "apps.write"
	AuthWrite      = "auth.write"
	IdPWrite       = "idp.write"
	SecurityWrite  = "security.write"
	SessionsManage = "sessions.manage"
	AuditRead      = "audit.read"
	VaultWrite     = "vault.write"
	BrandingWrite  = "branding.write"
	SystemWrite    = "system.write"
	Everything     = "*"
)

// All lists every permission with what it allows, for the role editor.
var All = []struct{ ID, Label string }{
	{UsersRead, "See users and groups"},
	{UsersWrite, "Add, change and remove users"},
	{GroupsWrite, "Manage groups"},
	{RolesWrite, "Manage roles and who has them"},
	{AppsRead, "See applications"},
	{AppsWrite, "Add and change applications"},
	{AuthWrite, "Authentication methods and MFA"},
	{IdPWrite, "Identity providers"},
	{SecurityWrite, "Security settings"},
	{SessionsManage, "See and end sessions"},
	{AuditRead, "Read the audit log"},
	{VaultWrite, "Vault providers and policy"},
	{BrandingWrite, "Branding"},
	{SystemWrite, "System settings"},
}

// Known reports whether p is a permission (or "*").
func Known(p string) bool {
	if p == Everything {
		return true
	}
	for _, x := range All {
		if x.ID == p {
			return true
		}
	}
	return false
}

// Grants is what one account may do.
type Grants struct {
	global map[string]bool
	apps   map[string]map[string]bool // app id → permissions there
	roles  []string                   // global role ids, for display
}

// Super reports whether g holds every permission.
func (g Grants) Super() bool { return g.global[Everything] }

// Has reports whether g holds p everywhere.
func (g Grants) Has(p string) bool { return g.global[Everything] || g.global[p] }

// HasFor reports whether g holds p for app (globally or for that app).
func (g Grants) HasFor(p, app string) bool { return g.Has(p) || g.apps[app][p] }

// Admin reports whether g opens the admin portal: any permission at all.
func (g Grants) Admin() bool { return len(g.global) > 0 || len(g.apps) > 0 }

// Covers reports whether g holds every permission in ps (globally): nobody
// hands out more than they have.
func (g Grants) Covers(ps []string) bool {
	for _, p := range ps {
		if p == Everything && !g.Super() || !g.Has(p) {
			return false
		}
	}
	return true
}

// AdminPermissions are the Administrator role's permissions (migration 0009).
var AdminPermissions = []string{UsersRead, UsersWrite, GroupsWrite, AppsRead, AppsWrite, AuthWrite, IdPWrite,
	SecurityWrite, SessionsManage, AuditRead, VaultWrite, BrandingWrite, SystemWrite}

// CoversApps reports whether g holds, for every app, the app-scoped
// permissions other holds there.
func (g Grants) CoversApps(other Grants) bool {
	for app, ps := range other.apps {
		for p := range ps {
			if !g.HasFor(p, app) {
				return false
			}
		}
	}
	return true
}

// List returns g's global permissions, sorted ("*" alone for a super admin).
func (g Grants) List() []string {
	if g.Super() {
		return []string{Everything}
	}
	out := make([]string, 0, len(g.global))
	for p := range g.global {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Roles returns the ids of g's global roles.
func (g Grants) Roles() []string { return g.roles }

// Apps returns the ids of the apps g has app-scoped permissions for.
func (g Grants) Apps() []string {
	out := make([]string, 0, len(g.apps))
	for a := range g.apps {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

// Owner is the owner's grants: everything.
func Owner() Grants {
	return Grants{global: map[string]bool{Everything: true}, roles: []string{"superadmin"}}
}

// Reader is satisfied by *pgxpool.Pool and pgx.Tx.
type Reader interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Load returns what account id ("owner" or a users.id) may do, through its
// own roles and its groups' roles.
func Load(ctx context.Context, q Reader, id string) (Grants, error) {
	if id == "owner" || id == "" {
		return Owner(), nil
	}
	rows, err := q.Query(ctx, `select r.id, r.permissions, ra.app_id::text from role_assignments ra
		join roles r on r.id = ra.role_id
		where ra.account_id = $1 or ra.group_id in (select group_id from group_members where account_id = $1)`, id)
	if err != nil {
		return Grants{}, err
	}
	defer rows.Close()
	g := Grants{global: map[string]bool{}, apps: map[string]map[string]bool{}}
	for rows.Next() {
		var role string
		var perms []string
		var app *string
		if err := rows.Scan(&role, &perms, &app); err != nil {
			return Grants{}, err
		}
		target := g.global
		if app != nil {
			if g.apps[*app] == nil {
				g.apps[*app] = map[string]bool{}
			}
			target = g.apps[*app]
		} else if !slices.Contains(g.roles, role) {
			g.roles = append(g.roles, role)
		}
		for _, p := range perms {
			target[p] = true
		}
	}
	slices.Sort(g.roles)
	return g, rows.Err()
}
