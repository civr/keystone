package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/onepassword"
	"github.com/civr/keystone/internal/rbac"
	"github.com/civr/keystone/internal/session"
)

// Credential providers (Admin › Vault & secrets): a 1Password Connect server
// whose items people can link into their vault. Keystone keeps the Connect
// address and its token sealed with vault_key, and per linked item only a
// reference; the secret is fetched when it is used, never stored. Admins
// with vault.write choose who may link items from a provider.

type provider struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Access []string `json:"access"`
	sealed string
}

func (s *server) loadProviders(ctx context.Context) ([]provider, error) {
	rows, err := s.d.Pool.Query(ctx, `select id::text, kind, name, url, access, token_sealed from vault_providers order by name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (provider, error) {
		var p provider
		return p, r.Scan(&p.ID, &p.Kind, &p.Name, &p.URL, &p.Access, &p.sealed)
	})
}

func (s *server) providerByID(ctx context.Context, id string) (provider, error) {
	list, err := s.loadProviders(ctx)
	if err != nil {
		return provider{}, err
	}
	for _, p := range list {
		if p.ID == id {
			return p, nil
		}
	}
	return provider{}, pgx.ErrNoRows
}

func (s *server) client(p provider) (*onepassword.Client, error) {
	tok, err := crypto.Open(s.d.Secrets.VaultKey, p.sealed)
	if err != nil {
		return nil, err
	}
	return &onepassword.Client{URL: p.URL, Token: string(tok)}, nil
}

// mayLink reports whether the signed-in account may use provider p.
func (s *server) mayLink(ctx context.Context, p provider) bool {
	if g, err := s.grants(ctx); err == nil && g.Has(rbac.VaultWrite) {
		return true
	}
	if p.Access == nil {
		return false
	}
	acct, err := s.account(ctx)
	if err != nil {
		return false
	}
	if slices.Contains(p.Access, acct.ID) {
		return true
	}
	for _, g := range acct.Groups {
		if slices.Contains(p.Access, "group:"+g) {
			return true
		}
	}
	return false
}

func (s *server) adminListProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.loadProviders(r.Context())
	if err != nil {
		errInternal(w, "list providers", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": list})
}

func (s *server) createProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Token  string   `json:"token"`
		Access []string `json:"access"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name, in.URL, in.Token = strings.TrimSpace(in.Name), strings.TrimSpace(in.URL), strings.TrimSpace(in.Token)
	switch {
	case !roleNameRe.MatchString(in.Name):
		writeInvalid(w, "name", "Give the provider a name of at most 60 characters.")
		return
	case !webURL(in.URL):
		writeInvalid(w, "url", "Enter the Connect server's address, such as http://op-connect:8080.")
		return
	case in.Token == "" || len(in.Token) > 8192:
		writeInvalid(w, "token", "Paste the Connect access token.")
		return
	}
	if !s.checkAccess(ctx, w, in.Access) {
		return
	}
	// The token must work before it is kept.
	c := &onepassword.Client{URL: in.URL, Token: in.Token}
	if _, err := c.Vaults(ctx); err != nil {
		writeInvalid(w, "token", "Connect did not accept this: "+err.Error())
		return
	}
	sealed, err := crypto.Seal(s.d.Secrets.VaultKey, []byte(in.Token))
	if err != nil {
		errInternal(w, "seal token", err)
		return
	}
	var id string
	err = s.d.Pool.QueryRow(ctx, `insert into vault_providers (kind, name, url, token_sealed, access) values ('onepassword', $1, $2, $3, $4)
		returning id::text`, in.Name, in.URL, sealed, in.Access).Scan(&id)
	if isUnique(err) {
		writeInvalid(w, "name", "A provider with this name exists.")
		return
	}
	if err != nil {
		errInternal(w, "create provider", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "vault.provider_added", clientIP(ctx), map[string]any{"id": id, "name": in.Name, "url": in.URL})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := appID(w, r)
	if !ok {
		return
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `select count(*) from vault_items where provider_id = $1`, id).Scan(&n); err != nil {
		errInternal(w, "count items", err)
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "in_use", "Vault items still link to this provider. Remove them first.")
		return
	}
	var name string
	err := s.d.Pool.QueryRow(ctx, `delete from vault_providers where id = $1 returning name`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		errNotFound(w, r)
		return
	}
	if err != nil {
		errInternal(w, "delete provider", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "vault.provider_removed", clientIP(ctx), map[string]any{"id": id, "name": name})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// myProviders lists the providers the account may link items from.
func (s *server) myProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.loadProviders(r.Context())
	if err != nil {
		errInternal(w, "list providers", err)
		return
	}
	out := []map[string]string{}
	for _, p := range list {
		if s.mayLink(r.Context(), p) {
			out = append(out, map[string]string{"id": p.ID, "name": p.Name})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

// providerItems lists item titles from a provider, for linking (no secrets).
func (s *server) providerItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.providerByID(ctx, chi.URLParam(r, "id"))
	if err != nil || !s.mayLink(ctx, p) {
		errNotFound(w, r)
		return
	}
	c, err := s.client(p)
	if err != nil {
		errInternal(w, "open token", err)
		return
	}
	vaults, err := c.Vaults(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "provider", "1Password Connect could not be reached: "+err.Error())
		return
	}
	type item struct {
		Ref, Title, Vault, Category string
	}
	out := []item{}
	for _, v := range vaults {
		items, err := c.Items(ctx, v.ID)
		if err != nil {
			continue
		}
		for _, it := range items {
			out = append(out, item{Ref: onepassword.Ref(v.ID, it.ID), Title: it.Title, Vault: v.Name, Category: it.Category})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// providerSecret fetches a linked item's secret.
func (s *server) providerSecret(ctx context.Context, providerID, ref string) (onepassword.Secret, error) {
	p, err := s.providerByID(ctx, providerID)
	if err != nil {
		return onepassword.Secret{}, err
	}
	// Access is checked on every use: taking someone off a provider also
	// stops the items they linked earlier.
	if !s.mayLink(ctx, p) {
		return onepassword.Secret{}, onepassword.ErrNotFound
	}
	c, err := s.client(p)
	if err != nil {
		return onepassword.Secret{}, err
	}
	vault, item, ok := onepassword.SplitRef(ref)
	if !ok {
		return onepassword.Secret{}, onepassword.ErrNotFound
	}
	return c.Get(ctx, vault, item)
}

// vaultLock locks the vault for this session until the next confirmation.
func (s *server) vaultLock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := *SessionFrom(ctx)
	sess.VaultLockedAt = time.Now()
	if err := s.d.Sessions.Update(ctx, sess); err != nil {
		errUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"open": false})
}

// vaultStatus says whether the vault is open and for how long.
func (s *server) vaultStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := *SessionFrom(ctx)
	window := time.Duration(settingsFrom(ctx).Vault.ReconfirmMinutes) * time.Minute
	now := time.Now()
	open := session.VaultOpen(sess, window, now)
	left := 0
	if open {
		last := sess.LastPasskeyAt
		if sess.NoPasskey && sess.LastFullAuthAt.After(last) {
			last = sess.LastFullAuthAt
		}
		left = int(last.Add(window).Sub(now).Seconds())
	}
	writeJSON(w, http.StatusOK, map[string]any{"open": open, "seconds_left": left})
}
