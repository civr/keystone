package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/crypto"
)

// --- sessions ---

func (s *server) listSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.d.Sessions.List(ctx)
	if err != nil {
		errUnavailable(w)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	own := sessionHandle(SessionFrom(ctx).ID)
	out := make([]map[string]any, 0, len(list))
	for _, x := range list {
		out = append(out, map[string]any{
			"id": sessionHandle(x.ID), "created_at": x.CreatedAt, "last_seen_at": x.LastSeenAt, "ip": x.IP,
			"user_agent": x.UserAgent, "emergency": x.Emergency, "current": sessionHandle(x.ID) == own,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// sessionHandle is the opaque public name of a session. The session ID itself
// is the ks_session cookie value and must never leave the server.
func sessionHandle(id string) string {
	sum := sha256.Sum256([]byte(id))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:22]
}

func (s *server) deleteSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	handle := chi.URLParam(r, "id")
	list, err := s.d.Sessions.List(ctx)
	if err != nil {
		errUnavailable(w)
		return
	}
	var target string
	for _, x := range list {
		if subtle.ConstantTimeCompare([]byte(sessionHandle(x.ID)), []byte(handle)) == 1 {
			target = x.ID
			break
		}
	}
	if target == "" {
		errNotFound(w, r)
		return
	}
	if err := s.d.Sessions.Revoke(ctx, target); err != nil {
		errUnavailable(w)
		return
	}
	own := target == SessionFrom(ctx).ID
	if own {
		setCookie(w, r, settingsFrom(ctx), SessionCookie, "", 0)
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "session.revoked", clientIP(ctx), map[string]any{"own": own})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- passkeys ---

const ceremonyAddPasskey = "addpk:"

func (s *server) listPasskeys(w http.ResponseWriter, r *http.Request) {
	rows, err := s.d.Pool.Query(r.Context(), `select id, name, created_at, last_used_at from passkeys order by created_at, id`)
	if err != nil {
		errInternal(w, "list passkeys", err)
		return
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (map[string]any, error) {
		var id []byte
		var name string
		var created time.Time
		var used *time.Time
		if err := r.Scan(&id, &name, &created, &used); err != nil {
			return nil, err
		}
		return map[string]any{"id": base64.RawURLEncoding.EncodeToString(id), "name": name, "created_at": created, "last_used_at": used}, nil
	})
	if err != nil {
		errInternal(w, "list passkeys", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": out})
}

func (s *server) passkeyAddBegin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	svc, err := s.passkeyService(settingsFrom(ctx))
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	opts, state, err := svc.BeginRegistration(ctx)
	if err != nil {
		errInternal(w, "begin registration", err)
		return
	}
	if err := s.bindCeremony(ctx, ceremonyAddPasskey+SessionFrom(ctx).ID, ceremonyRegister, state); err != nil {
		errUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *server) passkeyAddFinish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, state, ok := s.takeCeremonyOr400(w, ctx, ceremonyAddPasskey+SessionFrom(ctx).ID)
	if !ok {
		return
	}
	svc, err := s.passkeyService(settingsFrom(ctx))
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	name := passkeyName(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := svc.FinishRegistration(ctx, state, r, name); err != nil {
		writeError(w, http.StatusBadRequest, "passkey_failed", "De passkey kon niet worden geregistreerd. Probeer het opnieuw.")
		return
	}
	// The first passkey of a session made without one: from now on only
	// passkeys confirm sensitive actions in it.
	if sess := *SessionFrom(ctx); sess.NoPasskey {
		sess.NoPasskey, sess.LastPasskeyAt = false, time.Now()
		if err := s.d.Sessions.Update(ctx, sess); err != nil {
			slog.Error("passkey: update session", "err", err)
		}
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "passkey.registered", clientIP(ctx), map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// passkeyID decodes the {id} URL parameter; ok is false when it is not base64url.
func passkeyID(r *http.Request) ([]byte, bool) {
	id, err := base64.RawURLEncoding.DecodeString(chi.URLParam(r, "id"))
	return id, err == nil && len(id) > 0
}

func (s *server) renamePasskey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxPasskeyNameRunes {
		writeInvalid(w, "name", "De naam moet 1 tot 64 tekens lang zijn.")
		return
	}
	id, ok := passkeyID(r)
	if !ok {
		errNotFound(w, r)
		return
	}
	tag, err := s.d.Pool.Exec(ctx, `update passkeys set name = $2 where id = $1`, id, name)
	if err != nil {
		errInternal(w, "rename passkey", err)
		return
	}
	if tag.RowsAffected() == 0 {
		errNotFound(w, r)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "passkey.renamed", clientIP(ctx), map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) deletePasskey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := passkeyID(r)
	if !ok {
		errNotFound(w, r)
		return
	}
	// Lock all rows so two concurrent deletes cannot both pass the count check.
	tx, err := s.d.Pool.Begin(ctx)
	if err != nil {
		errInternal(w, "delete passkey", err)
		return
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := tx.Query(ctx, `select id from passkeys for update`)
	if err != nil {
		errInternal(w, "delete passkey", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		errInternal(w, "delete passkey", err)
		return
	}
	found := false
	for _, x := range ids {
		found = found || string(x) == string(id)
	}
	switch {
	case !found:
		errNotFound(w, r)
		return
	case len(ids) <= 1:
		writeError(w, http.StatusConflict, "last_passkey", "Dit is je laatste passkey en kan niet worden verwijderd.")
		return
	}
	if _, err := tx.Exec(ctx, `delete from passkeys where id = $1`, id); err != nil {
		errInternal(w, "delete passkey", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		errInternal(w, "delete passkey", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "passkey.removed", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- recovery codes and emergency account ---

func (s *server) regenerateRecovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	codes := make([]string, recoveryCodeCount)
	hashes := make([]string, recoveryCodeCount)
	for i := range codes {
		codes[i] = crypto.RecoveryCode()
		h, err := crypto.HashSecret(codes[i])
		if err != nil {
			errInternal(w, "hash recovery code", err)
			return
		}
		hashes[i] = h
	}
	tx, err := s.d.Pool.Begin(ctx)
	if err != nil {
		errInternal(w, "regenerate recovery codes", err)
		return
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `delete from recovery_codes`); err != nil {
		errInternal(w, "regenerate recovery codes", err)
		return
	}
	batch := &pgx.Batch{}
	for _, h := range hashes {
		batch.Queue(`insert into recovery_codes (hash) values ($1)`, h)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		errInternal(w, "regenerate recovery codes", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		errInternal(w, "regenerate recovery codes", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "recovery.regenerated", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (s *server) getEmergency(w http.ResponseWriter, r *http.Request) {
	var enabled, set bool
	err := s.d.Pool.QueryRow(r.Context(), `select coalesce((select enabled from emergency where id = 1), false),
		coalesce((select password_hash is not null and password_hash <> '' from emergency where id = 1), false)`).Scan(&enabled, &set)
	if err != nil {
		errInternal(w, "load emergency", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled, "password_set": set})
}

func (s *server) putEmergency(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Enabled  *bool  `json:"enabled"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		writeInvalid(w, "enabled", "enabled is verplicht.")
		return
	}
	enabled := *req.Enabled
	var hash *string
	if req.Password != "" {
		if utf8.RuneCountInString(req.Password) < emergencyMinLen {
			writeError(w, http.StatusBadRequest, "weak_password", "Het noodwachtwoord moet minstens 16 tekens hebben.")
			return
		}
		h, err := crypto.HashSecret(req.Password)
		if err != nil {
			errInternal(w, "hash emergency password", err)
			return
		}
		hash = &h
	}
	has, err := s.emergencyHasHash(ctx)
	if err != nil {
		errInternal(w, "load emergency", err)
		return
	}
	if enabled && hash == nil && !has {
		writeError(w, http.StatusBadRequest, "password_required", "Stel eerst een noodwachtwoord in.")
		return
	}
	var wasEnabled bool
	_ = s.d.Pool.QueryRow(ctx, `select coalesce((select enabled from emergency where id = 1), false)`).Scan(&wasEnabled)
	if _, err := s.d.Pool.Exec(ctx, `insert into emergency (id, enabled, password_hash) values (1, $1, $2)
		on conflict (id) do update set enabled = excluded.enabled,
		password_hash = coalesce(excluded.password_hash, emergency.password_hash)`, enabled, hash); err != nil {
		errInternal(w, "save emergency", err)
		return
	}
	if !enabled || hash != nil {
		s.revokeEmergencySessions(ctx)
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "emergency.changed", clientIP(ctx),
		map[string]any{"old_enabled": wasEnabled, "new_enabled": enabled, "password_changed": hash != nil})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// revokeEmergencySessions ends every session that was opened through the
// emergency account.
func (s *server) revokeEmergencySessions(ctx context.Context) {
	list, err := s.d.Sessions.List(ctx)
	if err != nil {
		slog.Error("admin: list sessions", "err", err)
		return
	}
	for _, x := range list {
		if x.Emergency {
			if err := s.d.Sessions.Revoke(ctx, x.ID); err != nil {
				slog.Error("admin: revoke emergency session", "err", err)
			}
		}
	}
}

func (s *server) emergencyHasHash(ctx context.Context) (bool, error) {
	var has bool
	err := s.d.Pool.QueryRow(ctx, `select coalesce((select password_hash is not null and password_hash <> '' from emergency where id = 1), false)`).Scan(&has)
	return has, err
}

// --- audit log ---

const (
	auditDefaultLimit = 50
	auditMaxLimit     = 200
)

type auditRow struct {
	ID     int64           `json:"id"`
	At     time.Time       `json:"at"`
	Actor  string          `json:"actor"`
	Action string          `json:"action"`
	IP     *string         `json:"ip"`
	Detail json.RawMessage `json:"detail"`
}

const auditCols = `id, at, actor, action, host(ip), detail`

func scanAudit(r pgx.CollectableRow) (auditRow, error) {
	var a auditRow
	return a, r.Scan(&a.ID, &a.At, &a.Actor, &a.Action, &a.IP, &a.Detail)
}

func (s *server) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, before := auditDefaultLimit, int64(0)
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > auditMaxLimit {
			writeInvalid(w, "limit", "limit moet tussen 1 en 200 liggen.")
			return
		}
		limit = n
	}
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			writeInvalid(w, "before", "before moet een positief getal zijn.")
			return
		}
		before = n
	}
	rows, err := s.d.Pool.Query(r.Context(), `select `+auditCols+` from audit_log
		where ($1::bigint = 0 or id < $1) order by id desc limit $2`, before, limit)
	if err != nil {
		errInternal(w, "list audit", err)
		return
	}
	entries, err := pgx.CollectRows(rows, scanAudit)
	if err != nil {
		errInternal(w, "list audit", err)
		return
	}
	res := map[string]any{"entries": entries, "next_before": nil}
	if len(entries) == limit {
		res["next_before"] = entries[len(entries)-1].ID
	}
	writeJSON(w, http.StatusOK, res)
}

// exportAudit streams every audit row as NDJSON, oldest first.
func (s *server) exportAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.d.Pool.Query(ctx, `select `+auditCols+` from audit_log order by id`)
	if err != nil {
		errInternal(w, "export audit", err)
		return
	}
	defer rows.Close()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // large exports outlive the server write timeout
	h := w.Header()
	h.Set("Content-Type", "application/x-ndjson; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Disposition", `attachment; filename="keystone-audit.ndjson"`)
	enc := json.NewEncoder(w)
	n := 0
	for rows.Next() {
		a, err := scanAudit(rows)
		if err != nil {
			slog.Error("admin: export audit scan", "err", err)
			return
		}
		if err := enc.Encode(a); err != nil {
			slog.Warn("admin: export audit write", "err", err)
			return
		}
		if n++; n%500 == 0 {
			_ = rc.Flush()
		}
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "audit.exported", clientIP(ctx), map[string]any{"rows": n})
}
