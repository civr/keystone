package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/apps"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/crypto"
)

// Vault (spec §5): credentials for apps without SAML/OIDC/LDAP. Passwords are
// sealed with vault_key, a secret file outside the database.

const maxVaultField = 1024

// vaultApp loads the app in the URL and checks it uses the vault.
func (s *server) vaultApp(w http.ResponseWriter, r *http.Request) (apps.App, bool) {
	id, ok := appID(w, r)
	if !ok {
		return apps.App{}, false
	}
	a, err := apps.ByID(r.Context(), s.d.Pool, id)
	if errors.Is(err, apps.ErrNotFound) {
		errNotFound(w, r)
		return a, false
	}
	if err != nil {
		errInternal(w, "vault app", err)
		return a, false
	}
	if a.Protocol != apps.Vault {
		writeInvalid(w, "protocol", "Alleen kluis-apps hebben opgeslagen inloggegevens.")
		return a, false
	}
	return a, true
}

func (s *server) getVault(w http.ResponseWriter, r *http.Request) {
	a, ok := s.vaultApp(w, r)
	if !ok {
		return
	}
	var user string
	var has bool
	err := s.d.Pool.QueryRow(r.Context(), `select username, password_sealed is not null from vault_entries where app_id = $1`, a.ID).Scan(&user, &has)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		errInternal(w, "vault get", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": user, "has_password": has})
}

func (s *server) putVault(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, ok := s.vaultApp(w, r)
	if !ok {
		return
	}
	var in struct {
		Username string  `json:"username"`
		Password *string `json:"password"` // omitted: keep; "": clear
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Username) > maxVaultField || (in.Password != nil && len(*in.Password) > maxVaultField) {
		writeInvalid(w, "password", "Maximaal 1024 tekens.")
		return
	}
	var err error
	if in.Password == nil {
		_, err = s.d.Pool.Exec(ctx, `insert into vault_entries (app_id, username, updated_at) values ($1, $2, now())
			on conflict (app_id) do update set username = excluded.username, updated_at = now()`, a.ID, in.Username)
	} else {
		var sealed *string
		if *in.Password != "" {
			v, serr := crypto.Seal(s.d.Secrets.VaultKey, []byte(*in.Password))
			if serr != nil {
				errInternal(w, "vault seal", serr)
				return
			}
			sealed = &v
		}
		_, err = s.d.Pool.Exec(ctx, `insert into vault_entries (app_id, username, password_sealed, updated_at) values ($1, $2, $3, now())
			on conflict (app_id) do update set username = excluded.username, password_sealed = excluded.password_sealed, updated_at = now()`,
			a.ID, in.Username, sealed)
	}
	if err != nil {
		errInternal(w, "vault put", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "vault.updated", clientIP(ctx), map[string]any{"id": a.ID, "app": a.Name, "password_changed": in.Password != nil})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// vaultPassword hands out the password for copying, only within
// vault.reconfirm_minutes of the last passkey confirmation.
func (s *server) vaultPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, ok := s.vaultApp(w, r)
	if !ok {
		return
	}
	cfg := settingsFrom(ctx)
	if time.Since(SessionFrom(ctx).LastPasskeyAt) > time.Duration(cfg.Vault.ReconfirmMinutes)*time.Minute {
		writeError(w, http.StatusForbidden, "stepup_required", "Bevestig dit eerst met je passkey.")
		return
	}
	var sealed *string
	err := s.d.Pool.QueryRow(ctx, `select password_sealed from vault_entries where app_id = $1`, a.ID).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && sealed == nil) {
		writeError(w, http.StatusNotFound, "no_password", "Voor deze app is geen wachtwoord opgeslagen.")
		return
	}
	if err != nil {
		errInternal(w, "vault password", err)
		return
	}
	pw, err := crypto.Open(s.d.Secrets.VaultKey, *sealed)
	if err != nil {
		errInternal(w, "vault open", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "vault.revealed", clientIP(ctx), map[string]any{"id": a.ID, "app": a.Name})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"password": string(pw)})
}
