package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/totp"
)

// Authenticator-app codes (TOTP) as a second step after the password. Once
// an account has confirmed one, every password sign-in (and a password
// step-up) also needs the current code; a code works once. Passkey-only
// sign-in on a trusted device and the reverse proxy skip it: the passkey
// or the proxy is the strong factor there. An admin who may manage the
// account can remove a lost authenticator.

type totpRow struct {
	sealed    string
	confirmed bool
	lastStep  int64
}

func (s *server) loadTOTP(ctx context.Context, account string) (totpRow, bool, error) {
	var t totpRow
	err := s.d.Pool.QueryRow(ctx, `select secret_sealed, confirmed_at is not null, last_step from totp where account_id = $1`,
		accountKey(account)).Scan(&t.sealed, &t.confirmed, &t.lastStep)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, false, nil
	}
	return t, err == nil, err
}

// totpEnabled reports whether account user has a confirmed authenticator.
func (s *server) totpEnabled(ctx context.Context, user string) (bool, error) {
	t, ok, err := s.loadTOTP(ctx, user)
	return ok && t.confirmed, err
}

// checkTOTP verifies code for account user and marks its step used. It
// returns false for a wrong, old or reused code.
func (s *server) checkTOTP(ctx context.Context, user, code string, confirmed bool) (bool, error) {
	t, ok, err := s.loadTOTP(ctx, user)
	if err != nil || !ok || t.confirmed != confirmed {
		return false, err
	}
	secret, err := crypto.Open(s.d.Secrets.DataKey, t.sealed)
	if err != nil {
		return false, err
	}
	step, good := totp.Verify(string(secret), code, time.Now(), t.lastStep)
	if !good {
		return false, nil
	}
	// The step moves forward only once: two requests with one code cannot both win.
	tag, err := s.d.Pool.Exec(ctx, `update totp set last_step = $2, confirmed_at = coalesce(confirmed_at, now())
		where account_id = $1 and last_step < $2`, accountKey(user), step)
	return err == nil && tag.RowsAffected() == 1, err
}

// authTOTP is the code step of a sign-in.
func (s *server) authTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	p, ok, err := s.loadPending(ctx, r)
	if err != nil {
		slog.Error("auth: load pending", "err", err)
		errUnavailable(w)
		return
	}
	if !ok || !p.passwordOK || !p.totpDue {
		errPasswordRequired(w)
		return
	}
	acct, err := accounts.Load(ctx, s.d.Pool, p.user)
	if err != nil {
		errInternal(w, "load account", err)
		return
	}
	if !s.allow(w, ctx, "login-ip", ip.String(), cfg.BruteForce.LoginPerIPPerMinute) ||
		!s.allow(w, ctx, "login-account", acct.Username, cfg.BruteForce.LoginPerAccountPerMinute) ||
		!s.notLocked(w, ctx, acct.Username, ip) {
		return
	}
	good, err := s.checkTOTP(ctx, p.user, req.Code, true)
	if err != nil {
		errInternal(w, "check code", err)
		return
	}
	if !good {
		s.d.Audit.Log(ctx, actorOf(p.user), "login.failure", ip, map[string]any{"method": "totp"})
		if _, err := s.d.Limiter.Fail(ctx, acct.Username, cfg); err != nil {
			slog.Error("auth: record failure", "err", err)
		}
		writeError(w, http.StatusUnauthorized, "bad_code", "That code is not right. Use the current code from your authenticator app.")
		return
	}
	if cfg.Login.PasskeyRequired {
		if err := s.d.Valkey.HSet(ctx, loginKey(p.token), "totp", false).Err(); err != nil {
			errUnavailable(w)
			return
		}
		next, err := s.nextStep(ctx, p.user)
		if err != nil {
			errInternal(w, "count passkeys", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"next": next})
		return
	}
	if fresh, err := s.consumePending(w, r, ctx, cfg, p); err != nil || !fresh {
		errPasswordRequired(w)
		return
	}
	passkeys, err := s.passkeyCount(ctx, p.user)
	if err != nil {
		errInternal(w, "count passkeys", err)
		return
	}
	now := time.Now()
	if !s.createSession(w, r, cfg, session.Session{UserID: p.user, LastFullAuthAt: now, NoPasskey: passkeys == 0, Emergency: p.emergency}) {
		return
	}
	s.markDevice(w, r, cfg, now)
	s.d.Audit.Log(ctx, actorOf(p.user), "login.success", ip, map[string]any{"method": "totp"})
	writeJSON(w, http.StatusOK, map[string]string{"next": "done"})
}

// --- the account's own authenticator ---

func (s *server) getMyTOTP(w http.ResponseWriter, r *http.Request) {
	t, ok, err := s.loadTOTP(r.Context(), SessionFrom(r.Context()).UserID)
	if err != nil {
		errInternal(w, "load totp", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": ok && t.confirmed})
}

// beginTOTP makes a new secret for the account; it counts once confirmed.
func (s *server) beginTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := SessionFrom(ctx).UserID
	if on, err := s.totpEnabled(ctx, user); err != nil || on {
		if err != nil {
			errInternal(w, "load totp", err)
		} else {
			writeError(w, http.StatusConflict, "totp_on", "An authenticator is already set up. Remove it first to add another.")
		}
		return
	}
	acct, err := s.account(ctx)
	if err != nil {
		errInternal(w, "load account", err)
		return
	}
	secret, err := totp.NewSecret()
	if err != nil {
		errInternal(w, "totp secret", err)
		return
	}
	sealed, err := crypto.Seal(s.d.Secrets.DataKey, []byte(secret))
	if err != nil {
		errInternal(w, "seal secret", err)
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `insert into totp (account_id, secret_sealed) values ($1, $2)
		on conflict (account_id) do update set secret_sealed = excluded.secret_sealed, confirmed_at = null, last_step = 0`,
		accountKey(user), sealed); err != nil {
		errInternal(w, "store totp", err)
		return
	}
	issuer := "Keystone"
	if b, err := s.branding.Get(ctx); err == nil {
		issuer = b.Name
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": totp.URI(issuer, acct.Username, secret)})
}

func (s *server) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user := SessionFrom(ctx).UserID
	if !s.allow(w, ctx, "totp-confirm", accountKey(user), 10) {
		return
	}
	good, err := s.checkTOTP(ctx, user, req.Code, false)
	if err != nil {
		errInternal(w, "check code", err)
		return
	}
	if !good {
		writeInvalid(w, "code", "That code is not right. Check the time on your phone and use the current code.")
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "totp.enabled", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
}

func (s *server) deleteMyTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, err := s.d.Pool.Exec(ctx, `delete from totp where account_id = $1`, accountKey(SessionFrom(ctx).UserID)); err != nil {
		errInternal(w, "delete totp", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "totp.disabled", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
}

// resetUserTOTP removes someone's authenticator (a lost phone).
func (s *server) resetUserTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if !s.mayManage(w, r, id) {
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `delete from totp where account_id = $1`, id); err != nil {
		errInternal(w, "delete totp", err)
		return
	}
	s.d.Audit.Log(ctx, actor(ctx), "totp.reset", clientIP(ctx), map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
