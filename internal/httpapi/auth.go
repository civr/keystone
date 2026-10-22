package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/gatetoken"
	"github.com/civr/keystone/internal/passkey"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

// Sign-in (spec §4). A login moves through a pending state in Valkey
// (login:<token>, 5 min, cookie ks_pending=login.<token>) with flags
// password_ok (AD or emergency password checked), emergency, and device
// (passkey-only attempt from a trusted device). The pending state is consumed
// when a session is created.

// DeviceCookie marks a trusted device: a signed device id plus the time of
// the last full (password + passkey) login.
const DeviceCookie = "ks_device"

const (
	loginKind = "login" // ks_pending value prefix
	loginTTL  = 5 * time.Minute

	ceremonyLogin    = "login"
	ceremonyRegister = "register"

	msgBadCredentials = "Gebruikersnaam of wachtwoord onjuist."
	msgADDown         = "AD is niet bereikbaar."
	msgRateLimited    = "Te veel pogingen. Wacht een minuut en probeer het opnieuw."
)

func (s *server) authRoutes(r chi.Router) {
	r.Use(s.requireSetupDone)
	r.Post("/password", s.authPassword)
	r.Post("/passkey/begin", s.authPasskeyBegin)
	r.Post("/passkey/finish", s.authPasskeyFinish)
	r.Post("/recovery", s.authRecovery)
	r.Post("/emergency", s.authEmergency)
	r.With(requireSession).Post("/stepup/begin", s.stepUpBegin)
	r.With(requireSession).Post("/stepup/finish", s.stepUpFinish)
	r.With(requireSession).Post("/logout", s.logout)
}

// requireSetupDone answers 409 setup_needed until setup has finished, so an
// owner stored mid-setup cannot sign in (and register a passkey) without the
// setup code.
// setupDone reports whether the setup wizard has been completed.
func (s *server) setupDone(ctx context.Context) (bool, error) {
	var done bool
	err := s.d.Pool.QueryRow(ctx,
		`select coalesce((select completed_at is not null from setup_state where id = 1), false)`).Scan(&done)
	return done, err
}

func (s *server) requireSetupDone(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done, err := s.setupDone(r.Context())
		if err != nil {
			slog.Error("auth: setup state", "err", err)
			errUnavailable(w)
			return
		}
		if !done {
			writeError(w, http.StatusConflict, "setup_needed", "Keystone is nog niet ingericht. Rond eerst de installatie af.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireStepUp rejects requests whose session has not used a passkey within
// login.step_up_minutes (403 stepup_required), or has no session (401).
func requireStepUp(next http.Handler) http.Handler {
	return requireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if needsStepUp(*SessionFrom(r.Context()), settingsFrom(r.Context()), time.Now()) {
			writeError(w, http.StatusForbidden, "stepup_required", "Bevestig dit eerst met je passkey.")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func needsStepUp(sess session.Session, cfg settings.Settings, now time.Time) bool {
	return sess.LastPasskeyAt.IsZero() ||
		now.Sub(sess.LastPasskeyAt) > time.Duration(cfg.Login.StepUpMinutes)*time.Minute
}

// mustRegisterGate blocks every API call of a session created with a
// recovery code, except passkey registration, logout and /me, until a new
// passkey is registered.
func mustRegisterGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := SessionFrom(r.Context())
		if sess == nil || !sess.MustRegisterPasskey || !pathUnder(r, "/api") || gateAllows(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusForbidden, "must_register_passkey", "Registreer eerst een nieuwe passkey.")
	})
}

func gateAllows(r *http.Request) bool {
	ok := func(p string) bool { return under(p, "/api/auth/passkey") || p == "/api/auth/logout" || p == "/api/me" }
	return ok(r.URL.Path) && (r.URL.RawPath == "" || ok(r.URL.RawPath))
}

// --- shared helpers ---

type ownerInfo struct{ Username, DisplayName, GivenName, Email string }

func (s *server) loadOwner(ctx context.Context) (ownerInfo, error) {
	var o ownerInfo
	err := s.d.Pool.QueryRow(ctx, `select username, coalesce(display_name, ''), coalesce(given_name, ''), coalesce(email, '')
		from owner where id = 1`).Scan(&o.Username, &o.DisplayName, &o.GivenName, &o.Email)
	return o, err
}

// emergencyAccount returns whether the emergency account is usable (enabled
// with a password hash) and its hash.
func (s *server) emergencyAccount(ctx context.Context) (bool, string, error) {
	var enabled bool
	var hash string
	err := s.d.Pool.QueryRow(ctx, `select enabled, coalesce(password_hash, '') from emergency where id = 1`).Scan(&enabled, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", nil
	}
	return enabled && hash != "", hash, err
}

// adContext bounds an AD call by ad.timeout_seconds, independent of the
// directory implementation.
func (s *server) adContext(ctx context.Context) (context.Context, context.CancelFunc) {
	var secs int
	if err := s.d.Pool.QueryRow(ctx, `select coalesce((data->>'timeout_seconds')::int, 0) from ad_config where id = 1`).Scan(&secs); err != nil || secs <= 0 {
		secs = ad.DefaultTimeout
	}
	return context.WithTimeout(ctx, time.Duration(secs)*time.Second)
}

func (s *server) directory() ad.Directory {
	if s.d.Dir == nil {
		return nil
	}
	return s.d.Dir()
}

// allow counts a hit against a per-minute limit; on refusal it has written the response.
func (s *server) allow(w http.ResponseWriter, ctx context.Context, scope, key string, perMinute int) bool {
	ok, err := s.d.Limiter.Allow(ctx, scope, key, perMinute)
	if err != nil {
		slog.Error("auth: rate limit", "err", err)
		errUnavailable(w)
		return false
	}
	if !ok {
		writeError(w, http.StatusTooManyRequests, "rate_limited", msgRateLimited)
	}
	return ok
}

func writeLocked(w http.ResponseWriter, ttl time.Duration) {
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error": "locked", "message": "Te veel mislukte pogingen. Dit account is tijdelijk geblokkeerd.",
		"retry_after": int(math.Ceil(ttl.Seconds())),
	})
}

// notLocked reports whether account may try to sign in; otherwise it has
// written 429 locked.
func (s *server) notLocked(w http.ResponseWriter, ctx context.Context, account string, ip netip.Addr) bool {
	locked, ttl, err := s.d.Limiter.Locked(ctx, account)
	if err != nil {
		slog.Error("auth: lock lookup", "err", err)
		errUnavailable(w)
		return false
	}
	if locked {
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.locked", ip, map[string]any{"username": account})
		writeLocked(w, ttl)
	}
	return !locked
}

// loginFailed records a failed attempt for account and answers 401, or 429
// when this failure triggered the lockout.
func (s *server) loginFailed(w http.ResponseWriter, ctx context.Context, cfg settings.Settings, account, method string, ip netip.Addr) {
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"username": account, "method": method})
	locked, err := s.d.Limiter.Fail(ctx, account, cfg)
	if err != nil {
		slog.Error("auth: record failure", "err", err)
	}
	if locked {
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.locked", ip, map[string]any{"username": account})
		writeLocked(w, time.Duration(cfg.BruteForce.LockoutMinutes)*time.Minute)
		return
	}
	writeError(w, http.StatusUnauthorized, "invalid_credentials", msgBadCredentials)
}

func (s *server) adUnavailable(w http.ResponseWriter, ctx context.Context) {
	enabled, _, err := s.emergencyAccount(ctx)
	if err != nil {
		slog.Error("auth: emergency lookup", "err", err)
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "ad_unavailable", "message": msgADDown, "emergency": enabled})
}

func errPasswordRequired(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, "password_required", "Log eerst in met je wachtwoord.")
}

// --- pending login state ---

type pendingLogin struct {
	token                         string
	passwordOK, emergency, device bool
}

func loginKey(token string) string { return "login:" + token }

func (s *server) newPending(w http.ResponseWriter, ctx context.Context, cfg settings.Settings, p pendingLogin) (pendingLogin, error) {
	tok, err := randomToken()
	if err != nil {
		return p, err
	}
	p.token = tok
	pipe := s.d.Valkey.TxPipeline()
	pipe.HSet(ctx, loginKey(tok), "password_ok", p.passwordOK, "emergency", p.emergency, "device", p.device)
	pipe.Expire(ctx, loginKey(tok), loginTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return p, err
	}
	setCookie(w, cfg, PendingCookie, loginKind+"."+tok, loginTTL)
	return p, nil
}

func (s *server) loadPending(ctx context.Context, r *http.Request) (pendingLogin, bool, error) {
	tok, ok := pendingToken(r, loginKind)
	if !ok {
		return pendingLogin{}, false, nil
	}
	h, err := s.d.Valkey.HGetAll(ctx, loginKey(tok)).Result()
	if err != nil || len(h) == 0 {
		return pendingLogin{}, false, err
	}
	return pendingLogin{token: tok, passwordOK: h["password_ok"] == "1", emergency: h["emergency"] == "1", device: h["device"] == "1"}, true, nil
}

// consumePending deletes the pending state; false when it was already gone
// (used by a concurrent request).
func (s *server) consumePending(w http.ResponseWriter, ctx context.Context, cfg settings.Settings, p pendingLogin) (bool, error) {
	n, err := s.d.Valkey.Del(ctx, loginKey(p.token)).Result()
	if err != nil {
		return false, err
	}
	setCookie(w, cfg, PendingCookie, "", 0)
	return n == 1, nil
}

// nextStep is the passkey step after a password: register when none exist.
func (s *server) nextStep(ctx context.Context) (string, error) {
	var n int
	if err := s.d.Pool.QueryRow(ctx, `select count(*) from passkeys`).Scan(&n); err != nil {
		return "", err
	}
	if n == 0 {
		return "register_passkey", nil
	}
	return "passkey", nil
}

// startPending creates a password_ok pending login and answers {"next": …}.
func (s *server) startPending(w http.ResponseWriter, ctx context.Context, cfg settings.Settings, emergency bool) {
	next, err := s.nextStep(ctx)
	if err != nil {
		errInternal(w, "count passkeys", err)
		return
	}
	if _, err := s.newPending(w, ctx, cfg, pendingLogin{passwordOK: true, emergency: emergency}); err != nil {
		slog.Error("auth: store pending", "err", err)
		errUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"next": next})
}

// --- sessions and devices ---

// createSession stores sess for this client and sets ks_session.
func (s *server) createSession(w http.ResponseWriter, r *http.Request, cfg settings.Settings, sess session.Session) bool {
	sess.IP, sess.UserAgent = clientIP(r.Context()).String(), r.UserAgent()
	created, err := s.d.Sessions.Create(r.Context(), sess, cfg)
	if err != nil {
		slog.Error("auth: create session", "err", err)
		errUnavailable(w)
		return false
	}
	setCookie(w, cfg, SessionCookie, created.ID, time.Duration(cfg.Session.DurationHours)*time.Hour)
	return true
}

// trustedDevice returns the last full login time of a valid ks_device cookie.
func (s *server) trustedDevice(r *http.Request, cfg settings.Settings) (string, time.Time, bool) {
	c, err := r.Cookie(DeviceCookie)
	if err != nil {
		return "", time.Time{}, false
	}
	return session.VerifyDeviceAt(s.d.Secrets.SessionKey, c.Value, cfg, time.Now())
}

// markDevice (re)signs ks_device after a full login, keeping a still-valid
// device id and otherwise creating a new one.
func (s *server) markDevice(w http.ResponseWriter, r *http.Request, cfg settings.Settings, fullAuth time.Time) {
	id, _, ok := s.trustedDevice(r, cfg)
	if !ok {
		var err error
		if id, err = randomToken(); err != nil {
			slog.Error("auth: device id", "err", err)
			return
		}
	}
	setCookie(w, cfg, DeviceCookie, session.SignDevice(s.d.Secrets.SessionKey, id, fullAuth),
		time.Duration(cfg.Session.TrustedDeviceDays)*24*time.Hour)
}

// --- handlers ---

func (s *server) authPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	account := ad.NormalizeUsername(req.Username)
	if !s.allow(w, ctx, "login-ip", ip.String(), cfg.BruteForce.LoginPerIPPerMinute) ||
		!s.allow(w, ctx, "login-account", account, cfg.BruteForce.LoginPerAccountPerMinute) ||
		!s.notLocked(w, ctx, account, ip) {
		return
	}
	dir := s.directory()
	if dir == nil {
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"username": account, "reason": "ad_unavailable"})
		s.adUnavailable(w, ctx)
		return
	}
	// Every account, owner or not, goes through the same AD bind so the
	// response time does not reveal who the owner is.
	actx, cancel := s.adContext(ctx)
	u, err := dir.Authenticate(actx, req.Username, req.Password)
	cancel()
	switch {
	case errors.Is(err, ad.ErrInvalidCredentials), errors.Is(err, ad.ErrNotFound):
		s.loginFailed(w, ctx, cfg, account, "password", ip)
		return
	case errors.Is(err, ad.ErrUnavailable):
		slog.Warn("auth: AD authenticate", "err", err)
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"username": account, "reason": "ad_unavailable"})
		s.adUnavailable(w, ctx)
		return
	case err != nil:
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"username": account, "reason": "ad_error"})
		errInternal(w, "AD authenticate", err)
		return
	}
	owner, err := s.loadOwner(ctx)
	if err != nil {
		errInternal(w, "load owner", err)
		return
	}
	if ad.NormalizeUsername(u.Username) != owner.Username {
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.not_allowed", ip, map[string]any{"username": ad.NormalizeUsername(u.Username)})
		writeError(w, http.StatusForbidden, "not_allowed", "Dit account mag niet inloggen bij Keystone.")
		return
	}
	if err := s.d.Limiter.ResetFailures(ctx, account); err != nil {
		slog.Error("auth: reset failures", "err", err)
	}
	if cfg.Login.PasskeyRequired {
		s.startPending(w, ctx, cfg, false)
		return
	}
	now := time.Now()
	if !s.createSession(w, r, cfg, session.Session{LastFullAuthAt: now}) {
		return
	}
	s.markDevice(w, r, cfg, now)
	s.d.Audit.Log(ctx, audit.ActorOwner, "login.success", ip, map[string]any{"method": "password"})
	writeJSON(w, http.StatusOK, map[string]string{"next": "done"})
}

func (s *server) authPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	svc, err := s.passkeyService(cfg)
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	var (
		bind, kind string
		opts       json.RawMessage
		state      []byte
	)
	if sess := SessionFrom(ctx); sess != nil && sess.MustRegisterPasskey {
		bind, kind = "register:"+sess.ID, ceremonyRegister
		opts, state, err = svc.BeginRegistration(ctx)
	} else {
		if !s.allow(w, ctx, "passkey-ip", clientIP(ctx).String(), cfg.BruteForce.LoginPerIPPerMinute) {
			return
		}
		p, ok, perr := s.loadPending(ctx, r)
		if perr != nil {
			slog.Error("auth: load pending", "err", perr)
			errUnavailable(w)
			return
		}
		switch {
		case ok && p.passwordOK:
			next, nerr := s.nextStep(ctx)
			if nerr != nil {
				errInternal(w, "count passkeys", nerr)
				return
			}
			bind = "login:" + p.token
			if next == "register_passkey" {
				kind = ceremonyRegister
				opts, state, err = svc.BeginRegistration(ctx)
			} else {
				kind = ceremonyLogin
				opts, state, err = svc.BeginLogin(ctx, false)
			}
		default:
			if _, _, trusted := s.trustedDevice(r, cfg); !trusted || !cfg.Login.PasskeyOnlyOnTrustedDevice {
				errPasswordRequired(w)
				return
			}
			if !ok || !p.device {
				if p, err = s.newPending(w, ctx, cfg, pendingLogin{device: true}); err != nil {
					slog.Error("auth: store pending", "err", err)
					errUnavailable(w)
					return
				}
			}
			bind, kind = "login:"+p.token, ceremonyLogin
			opts, state, err = svc.BeginLogin(ctx, true)
		}
	}
	if err != nil {
		errInternal(w, "begin passkey ceremony", err)
		return
	}
	if err := s.bindCeremony(ctx, bind, kind, state); err != nil {
		slog.Error("auth: store ceremony", "err", err)
		errUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

// takeCeremonyOr400 loads the ceremony bound to bind; on failure it has written the response.
func (s *server) takeCeremonyOr400(w http.ResponseWriter, ctx context.Context, bind string) (string, []byte, bool) {
	kind, state, err := s.takeBoundCeremony(ctx, bind)
	if errors.Is(err, errNoCeremony) {
		writeError(w, http.StatusBadRequest, "no_ceremony", "Start de passkey-stap opnieuw.")
		return "", nil, false
	}
	if err != nil {
		slog.Error("auth: take ceremony", "err", err)
		errUnavailable(w)
		return "", nil, false
	}
	return kind, state, true
}

func errPasskeyFailed(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, "passkey_failed", "De passkey kon niet worden gecontroleerd. Probeer het opnieuw.")
}

func (s *server) authPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)
	svc, err := s.passkeyService(cfg)
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)

	if sess := SessionFrom(ctx); sess != nil && sess.MustRegisterPasskey {
		s.finishForcedRegistration(w, r, cfg, svc, *sess)
		return
	}

	p, ok, err := s.loadPending(ctx, r)
	if err != nil {
		slog.Error("auth: load pending", "err", err)
		errUnavailable(w)
		return
	}
	if !ok || !(p.passwordOK || p.device) {
		errPasswordRequired(w)
		return
	}
	var fullAuth time.Time
	if !p.passwordOK {
		_, at, trusted := s.trustedDevice(r, cfg)
		if !trusted || !cfg.Login.PasskeyOnlyOnTrustedDevice {
			errPasswordRequired(w)
			return
		}
		fullAuth = at
	}
	kind, state, ok := s.takeCeremonyOr400(w, ctx, "login:"+p.token)
	if !ok {
		return
	}
	method := "passkey"
	if kind == ceremonyRegister {
		method = "passkey_register"
		err = svc.FinishRegistration(ctx, state, r, passkeyName(r))
	} else {
		err = svc.FinishLogin(ctx, state, r)
	}
	if err != nil {
		slog.Warn("auth: passkey", "kind", kind, "err", err)
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"method": method})
		errPasskeyFailed(w)
		return
	}
	if fresh, err := s.consumePending(w, ctx, cfg, p); err != nil || !fresh {
		if err != nil {
			slog.Error("auth: consume pending", "err", err)
		}
		errPasswordRequired(w)
		return
	}
	now := time.Now()
	full := p.passwordOK
	if full {
		fullAuth = now
	} else {
		method = "trusted_device"
	}
	if !s.createSession(w, r, cfg, session.Session{LastFullAuthAt: fullAuth, LastPasskeyAt: now, Emergency: p.emergency}) {
		return
	}
	if full {
		s.markDevice(w, r, cfg, now)
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "login.success", ip, map[string]any{"method": method, "emergency": p.emergency})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// finishForcedRegistration completes the passkey registration a recovery
// session must do, lifting the gate.
func (s *server) finishForcedRegistration(w http.ResponseWriter, r *http.Request, cfg settings.Settings, svc *passkey.Service, sess session.Session) {
	ctx := r.Context()
	_, state, ok := s.takeCeremonyOr400(w, ctx, "register:"+sess.ID)
	if !ok {
		return
	}
	name := passkeyName(r)
	if err := svc.FinishRegistration(ctx, state, r, name); err != nil {
		slog.Warn("auth: forced registration", "err", err)
		writeError(w, http.StatusBadRequest, "passkey_failed", "De passkey kon niet worden geregistreerd. Probeer het opnieuw.")
		return
	}
	now := time.Now()
	sess.MustRegisterPasskey, sess.LastPasskeyAt = false, now
	if err := s.d.Sessions.Update(ctx, sess); err != nil {
		slog.Error("auth: update session", "err", err)
		errUnavailable(w)
		return
	}
	s.markDevice(w, r, cfg, now)
	s.d.Audit.Log(ctx, audit.ActorOwner, "passkey.registered", clientIP(ctx), map[string]any{"name": name, "after": "recovery"})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) authRecovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.allow(w, ctx, "login-recovery", ip.String(), cfg.BruteForce.LoginPerIPPerMinute) {
		return
	}
	p, ok, err := s.loadPending(ctx, r)
	if err != nil {
		slog.Error("auth: load pending", "err", err)
		errUnavailable(w)
		return
	}
	if !ok || !p.passwordOK {
		errPasswordRequired(w)
		return
	}
	id, err := s.matchRecoveryCode(ctx, strings.ToUpper(strings.TrimSpace(req.Code)))
	if err != nil {
		errInternal(w, "recovery codes", err)
		return
	}
	invalid := func() {
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"method": "recovery"})
		writeError(w, http.StatusUnauthorized, "invalid_code", "Deze herstelcode is ongeldig of al gebruikt.")
	}
	if id == 0 {
		invalid()
		return
	}
	if fresh, err := s.consumePending(w, ctx, cfg, p); err != nil || !fresh {
		if err != nil {
			slog.Error("auth: consume pending", "err", err)
		}
		errPasswordRequired(w)
		return
	}
	tag, err := s.d.Pool.Exec(ctx, `update recovery_codes set used_at = now() where id = $1 and used_at is null`, id)
	if err != nil {
		errInternal(w, "use recovery code", err)
		return
	}
	if tag.RowsAffected() != 1 { // used concurrently
		invalid()
		return
	}
	if !s.createSession(w, r, cfg, session.Session{LastFullAuthAt: time.Now(), MustRegisterPasskey: true, Emergency: p.emergency}) {
		return
	}
	var left int
	if err := s.d.Pool.QueryRow(ctx, `select count(*) from recovery_codes where used_at is null`).Scan(&left); err != nil {
		slog.Error("auth: count recovery codes", "err", err)
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "recovery.used", ip, map[string]any{"remaining": left})
	writeJSON(w, http.StatusOK, map[string]string{"next": "register_passkey"})
}

// matchRecoveryCode returns the id of the unused recovery code equal to code, or 0.
func (s *server) matchRecoveryCode(ctx context.Context, code string) (int, error) {
	rows, err := s.d.Pool.Query(ctx, `select id, hash from recovery_codes where used_at is null order by id`)
	if err != nil {
		return 0, err
	}
	type row struct {
		id   int
		hash string
	}
	codes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.id, &x.hash)
	})
	if err != nil {
		return 0, err
	}
	for _, c := range codes {
		if crypto.VerifySecret(c.hash, code) {
			return c.id, nil
		}
	}
	return 0, nil
}

func (s *server) authEmergency(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !s.allow(w, ctx, "login-ip", ip.String(), cfg.BruteForce.LoginPerIPPerMinute) {
		return
	}
	enabled, hash, err := s.emergencyAccount(ctx)
	if err != nil {
		errInternal(w, "emergency lookup", err)
		return
	}
	if !enabled {
		writeError(w, http.StatusForbidden, "emergency_disabled", "Het nood-account is niet ingeschakeld.")
		return
	}
	owner, err := s.loadOwner(ctx)
	if err != nil {
		errInternal(w, "load owner", err)
		return
	}
	if !s.notLocked(w, ctx, owner.Username, ip) {
		return
	}
	// Only while AD cannot be reached: the emergency password is never
	// checked otherwise, so it cannot be guessed while AD is up.
	if dir := s.directory(); dir != nil {
		actx, cancel := s.adContext(ctx)
		_, err := dir.Lookup(actx, owner.Username)
		cancel()
		switch {
		case errors.Is(err, ad.ErrUnavailable):
		case err == nil, errors.Is(err, ad.ErrNotFound):
			writeError(w, http.StatusForbidden, "ad_available", "AD is bereikbaar. Log in met je gewone wachtwoord.")
			return
		default:
			errInternal(w, "AD lookup", err)
			return
		}
	}
	if !crypto.VerifySecret(hash, req.Password) {
		s.loginFailed(w, ctx, cfg, owner.Username, "emergency", ip)
		return
	}
	if err := s.d.Limiter.ResetFailures(ctx, owner.Username); err != nil {
		slog.Error("auth: reset failures", "err", err)
	}
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "emergency.used", ip, nil)
	s.startPending(w, ctx, cfg, true)
}

func (s *server) stepUpBegin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	svc, err := s.passkeyService(settingsFrom(ctx))
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	opts, state, err := svc.BeginLogin(ctx, false)
	if err != nil {
		errInternal(w, "begin step-up", err)
		return
	}
	if err := s.bindCeremony(ctx, "stepup:"+SessionFrom(ctx).ID, ceremonyLogin, state); err != nil {
		slog.Error("auth: store ceremony", "err", err)
		errUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *server) stepUpFinish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := clientIP(ctx)
	sess := *SessionFrom(ctx)
	_, state, ok := s.takeCeremonyOr400(w, ctx, "stepup:"+sess.ID)
	if !ok {
		return
	}
	svc, err := s.passkeyService(settingsFrom(ctx))
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := svc.FinishLogin(ctx, state, r); err != nil {
		slog.Warn("auth: step-up", "err", err)
		s.d.Audit.Log(ctx, audit.ActorOwner, "login.failure", ip, map[string]any{"method": "stepup"})
		errPasskeyFailed(w)
		return
	}
	sess.LastPasskeyAt = time.Now()
	if err := s.d.Sessions.Update(ctx, sess); err != nil {
		slog.Error("auth: update session", "err", err)
		errUnavailable(w)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorOwner, "stepup", ip, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	if err := s.d.Sessions.Revoke(ctx, SessionFrom(ctx).ID); err != nil {
		slog.Error("auth: revoke session", "err", err)
		errUnavailable(w)
		return
	}
	if err := gatetoken.RevokeSession(ctx, s.d.Valkey, SessionFrom(ctx).ID); err != nil {
		slog.Warn("auth: revoke gate tokens", "err", err) // they die with the session anyway
	}
	setCookie(w, cfg, SessionCookie, "", 0)
	s.d.Audit.Log(ctx, audit.ActorOwner, "logout", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	o, err := s.loadOwner(ctx)
	if err != nil {
		errInternal(w, "load owner", err)
		return
	}
	sess := SessionFrom(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"username": o.Username, "display_name": o.DisplayName, "given_name": o.GivenName, "email": o.Email,
		"emergency": sess.Emergency, "must_register_passkey": sess.MustRegisterPasskey,
		"needs_stepup": needsStepUp(*sess, settingsFrom(ctx), time.Now()),
		"client_ip":    clientIP(ctx).String(),
	})
}
