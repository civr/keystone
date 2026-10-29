package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/onepassword"
	"github.com/civr/keystone/internal/session"
)

// The vault's own items (Kluis): sign-in details for anything, not tied to an
// app tile, private to the account that adds them. Password and note are
// sealed with vault_key; the list shows only what is safe to show, and
// revealing follows the vault's reconfirm window.

const (
	maxVaultTitle = 100
	maxVaultNote  = 4096
)

// vaultRoutes mounts /api/vault: reading the list needs a session, changes a
// step-up, revealing a confirmation within vault.reconfirm_minutes.
func (s *server) vaultRoutes(r chi.Router) {
	g := r.With(s.requireSetupDone, requireSession)
	w := g.With(requireStepUp)
	g.Get("/", s.listVaultItems)
	w.Post("/", s.createVaultItem)
	w.Put("/{id}", s.updateVaultItem)
	w.Delete("/{id}", s.deleteVaultItem)
	g.Post("/{id}/reveal", s.revealVaultItem)
	g.Get("/status", s.vaultStatus)
	g.Post("/lock", s.vaultLock)
	g.Get("/providers", s.myProviders)
	g.Get("/providers/{id}/items", s.providerItems)
}

type vaultItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Username    string    `json:"username"`
	HasPassword bool      `json:"has_password"`
	HasNote     bool      `json:"has_note"`
	Kind        string    `json:"kind"`
	Provider    string    `json:"provider,omitempty"` // provider id when linked
	UpdatedAt   time.Time `json:"updated_at"`
}

type vaultItemInput struct {
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Username string  `json:"username"`
	Password *string `json:"password"` // omitted: keep; "": clear
	Note     *string `json:"note"`     // omitted: keep; "": clear
	Kind     string  `json:"kind"`
	// Provider and Ref link a 1Password item instead of storing a secret.
	Provider string `json:"provider"`
	Ref      string `json:"ref"`
}

var vaultKinds = []string{"login", "api_key", "token", "client_secret", "note"}

// readVaultItem decodes and checks an item; false when an answer was written.
func readVaultItem(w http.ResponseWriter, r *http.Request) (vaultItemInput, bool) {
	var in vaultItemInput
	if !readJSONLimit(w, r, &in, 16<<10) {
		return in, false
	}
	in.Title, in.URL, in.Username = strings.TrimSpace(in.Title), strings.TrimSpace(in.URL), strings.TrimSpace(in.Username)
	switch {
	case in.Title == "":
		writeInvalid(w, "title", "Give the item a name.")
	case utf8.RuneCountInString(in.Title) > maxVaultTitle:
		writeInvalid(w, "title", "The name may be at most 100 characters.")
	case in.URL != "" && !webURL(in.URL):
		writeInvalid(w, "url", "Enter a web address starting with http:// or https://.")
	case len(in.URL) > maxVaultField || len(in.Username) > maxVaultField:
		writeInvalid(w, "username", "At most 1024 characters.")
	case in.Password != nil && len(*in.Password) > maxVaultField:
		writeInvalid(w, "password", "At most 1024 characters.")
	case in.Note != nil && len(*in.Note) > maxVaultNote:
		writeInvalid(w, "note", "The note may be at most 4096 characters.")
	case in.Kind != "" && !slices.Contains(vaultKinds, in.Kind):
		writeInvalid(w, "kind", "Choose what kind of secret this is.")
	default:
		return in, true
	}
	return in, false
}

func webURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// seal returns the sealed value for an optional secret: nil for "".
func (s *server) seal(v string) (*string, error) {
	if v == "" {
		return nil, nil
	}
	sealed, err := crypto.Seal(s.d.Secrets.VaultKey, []byte(v))
	return &sealed, err
}

// vaultOwner is the vault_items.user_id of the signed-in account (nil: the owner).
func vaultOwner(r *http.Request) *string { return accounts.Ref(SessionFrom(r.Context()).UserID) }

func (s *server) listVaultItems(w http.ResponseWriter, r *http.Request) {
	rows, err := s.d.Pool.Query(r.Context(), `select id, title, url, username, password_sealed is not null or provider_id is not null,
		note_sealed is not null, kind, coalesce(provider_id::text, ''), updated_at from vault_items where user_id is not distinct from $1
		order by lower(title), id`, vaultOwner(r))
	if err != nil {
		errInternal(w, "vault list", err)
		return
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (vaultItem, error) {
		var it vaultItem
		err := row.Scan(&it.ID, &it.Title, &it.URL, &it.Username, &it.HasPassword, &it.HasNote, &it.Kind, &it.Provider, &it.UpdatedAt)
		return it, err
	})
	if err != nil {
		errInternal(w, "vault list", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) createVaultItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, ok := readVaultItem(w, r)
	if !ok {
		return
	}
	pw, err := s.seal(deref(in.Password))
	if err != nil {
		errInternal(w, "vault seal", err)
		return
	}
	note, err := s.seal(deref(in.Note))
	if err != nil {
		errInternal(w, "vault seal", err)
		return
	}
	var it vaultItem
	if in.Kind == "" {
		in.Kind = "login"
	}
	var prov *string
	if in.Provider != "" {
		p, err := s.providerByID(ctx, in.Provider)
		if err != nil || !s.mayLink(ctx, p) {
			writeInvalid(w, "provider", "You cannot link items from this provider.")
			return
		}
		if _, _, ok := onepassword.SplitRef(in.Ref); !ok {
			writeInvalid(w, "ref", "Choose an item.")
			return
		}
		prov, pw, note = &p.ID, nil, nil // nothing secret is stored for a linked item
	}
	err = s.d.Pool.QueryRow(ctx, `insert into vault_items (title, url, username, password_sealed, note_sealed, user_id, kind, provider_id, provider_ref)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9) returning id, title, url, username, password_sealed is not null or provider_id is not null,
		note_sealed is not null, kind, coalesce(provider_id::text, ''), updated_at`,
		in.Title, in.URL, in.Username, pw, note, vaultOwner(r), in.Kind, prov, in.Ref).Scan(&it.ID, &it.Title, &it.URL, &it.Username, &it.HasPassword, &it.HasNote, &it.Kind, &it.Provider, &it.UpdatedAt)
	if err != nil {
		errInternal(w, "vault create", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "vault.item_created", clientIP(ctx), map[string]any{"id": it.ID, "title": it.Title})
	writeJSON(w, http.StatusCreated, it)
}

func (s *server) updateVaultItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	in, ok := readVaultItem(w, r)
	if !ok {
		return
	}
	// An omitted secret ($5/$7 false) keeps the stored one.
	pw, err := s.seal(deref(in.Password))
	if err != nil {
		errInternal(w, "vault seal", err)
		return
	}
	note, err := s.seal(deref(in.Note))
	if err != nil {
		errInternal(w, "vault seal", err)
		return
	}
	var it vaultItem
	err = s.d.Pool.QueryRow(ctx, `update vault_items set title = $2, url = $3, username = $4,
		password_sealed = case when $5 then $6 else password_sealed end,
		note_sealed = case when $7 then $8 else note_sealed end,
		kind = coalesce(nullif($10, ''), kind),
		updated_at = now()
		where id = $1 and user_id is not distinct from $9 and (provider_id is null or not ($5 or $7))
		returning id, title, url, username, password_sealed is not null or provider_id is not null, note_sealed is not null, kind,
		coalesce(provider_id::text, ''), updated_at`,
		id, in.Title, in.URL, in.Username, in.Password != nil, pw, in.Note != nil, note, vaultOwner(r), in.Kind).
		Scan(&it.ID, &it.Title, &it.URL, &it.Username, &it.HasPassword, &it.HasNote, &it.Kind, &it.Provider, &it.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "vault update", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "vault.item_updated", clientIP(ctx), map[string]any{"id": it.ID, "title": it.Title, "password_changed": in.Password != nil})
	writeJSON(w, http.StatusOK, it)
}

func (s *server) deleteVaultItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	var title string
	err := s.d.Pool.QueryRow(ctx, `delete from vault_items where id = $1 and user_id is not distinct from $2 returning title`,
		id, vaultOwner(r)).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "vault delete", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "vault.item_deleted", clientIP(ctx), map[string]any{"id": id, "title": title})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// revealVaultItem hands out the password and note, only within the vault's
// reconfirm window (like an app's vault password).
func (s *server) revealVaultItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	cfg := settingsFrom(ctx)
	if !session.VaultOpen(*SessionFrom(ctx), time.Duration(cfg.Vault.ReconfirmMinutes)*time.Minute, time.Now()) {
		writeError(w, http.StatusForbidden, "stepup_required", "Confirm this with your passkey first.")
		return
	}
	var title, provID, ref string
	var pw, note *string
	err := s.d.Pool.QueryRow(ctx, `select title, password_sealed, note_sealed, coalesce(provider_id::text, ''), provider_ref from vault_items
		where id = $1 and user_id is not distinct from $2`, id, vaultOwner(r)).Scan(&title, &pw, &note, &provID, &ref)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "vault reveal", err)
		return
	}
	out := map[string]string{"password": "", "note": ""}
	if provID != "" {
		sec, err := s.providerSecret(ctx, provID, ref)
		if err != nil {
			writeError(w, http.StatusBadGateway, "provider", "The item could not be read from 1Password: "+err.Error())
			return
		}
		out = map[string]string{"password": sec.Password, "note": sec.Note, "username": sec.Username}
		pw, note = nil, nil
	}
	for k, v := range map[string]*string{"password": pw, "note": note} {
		if v == nil {
			continue
		}
		plain, err := crypto.Open(s.d.Secrets.VaultKey, *v)
		if err != nil {
			errInternal(w, "vault open", err)
			return
		}
		out[k] = string(plain)
	}
	s.d.Audit.Log(ctx, actor(ctx), "vault.revealed", clientIP(ctx), map[string]any{"id": id, "title": title})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
