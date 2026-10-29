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

	"github.com/civr/keystone/internal/accounts"
	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/apps"
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

	msgBadCredentials = "Wrong username or password."
	msgADDown         = "Active Directory cannot be reached."
	msgRateLimited    = "Too many attempts. Wait a minute and try again."
)

func (s *server) authRoutes(r chi.Router) {
	r.Use(s.requireSetupDone)
	r.Post("/password", s.authPassword)
	r.Post("/passkey/begin", s.authPasskeyBegin)
	r.Post("/passkey/finish", s.authPasskeyFinish)
	r.Post("/recovery", s.authRecovery)
	r.Post("/emergency", s.authEmergency)
	r.Post("/proxy", s.authProxy)
	r.Post("/totp", s.authTOTP)
	r.With(requireSession).Post("/stepup/begin", s.stepUpBegin)
	r.With(requireSession).Post("/stepup/finish", s.stepUpFinish)
	r.With(requireSession).Post("/stepup/password", s.stepUpPassword)
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
			writeError(w, http.StatusConflict, "setup_needed", "Keystone is not set up yet. Finish the setup first.")
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
			writeError(w, http.StatusForbidden, "stepup_required", "Confirm this with your passkey first.")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// needsStepUp reports whether a sensitive action needs a fresh confirmation.
func needsStepUp(sess session.Session, cfg settings.Settings, now time.Time) bool {
	return !confirmedWithin(sess, time.Duration(cfg.Login.StepUpMinutes)*time.Minute, now)
}

// confirmedWithin reports whether sess was confirmed in the last d: by a
// passkey, or — while no passkey exists — by the password.
func confirmedWithin(sess session.Session, d time.Duration, now time.Time) bool {
	last := sess.LastPasskeyAt
	if sess.NoPasskey && sess.LastFullAuthAt.After(last) {
		last = sess.LastFullAuthAt
	}
	return !last.IsZero() && now.Sub(last) <= d
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
		writeError(w, http.StatusForbidden, "must_register_passkey", "Register a new passkey first.")
	})
}

func gateAllows(r *http.Request) bool {
	ok := func(p string) bool { return under(p, "/api/auth/passkey") || p == "/api/auth/logout" || p == "/api/me" }
	return ok(r.URL.Path) && (r.URL.RawPath == "" || ok(r.URL.RawPath))
}

// --- shared helpers ---

// passkeyCount is the number of passkeys of account user ("" is the owner).
func (s *server) passkeyCount(ctx context.Context, user string) (int, error) {
	var n int
	err := s.d.Pool.QueryRow(ctx, `select count(*) from passkeys where user_id is not distinct from $1`, accounts.Ref(user)).Scan(&n)
	return n, err
}

// mayUse reports whether the signed-in account may use app a.
func (s *server) mayUse(ctx context.Context, a apps.App) bool {
	acct, err := s.account(ctx)
	return err == nil && a.AllowedFor(acct)
}

// msgNeedsMFA explains a refusal by an app that requires a second factor.
const msgNeedsMFA = "This app needs a passkey or an authenticator code at sign-in. Sign out and sign in again with one."

// lacksMFA reports whether app a requires a second factor this session did not use.
func lacksMFA(ctx context.Context, a apps.App) bool {
	sess := SessionFrom(ctx)
	return a.Config.RequireMFA && (sess == nil || !sess.MFA())
}

// vaultOpen reports whether the vault is unlocked in sess: confirmed within
// the vault's window, and after any manual lock.
func vaultOpen(sess session.Session, d time.Duration, now time.Time) bool {
	last := sess.LastPasskeyAt
	if sess.NoPasskey && sess.LastFullAuthAt.After(last) {
		last = sess.LastFullAuthAt
	}
	return confirmedWithin(sess, d, now) && last.After(sess.VaultLockedAt)
}

// accountKey turns a session's UserID into an account id ("" → owner).
func accountKey(user string) string {
	if user == "" {
		return accounts.OwnerID
	}
	return user
}

// accountRef turns an account id into a session's UserID (owner → "").
func accountRef(id string) string {
	if id == accounts.OwnerID {
		return ""
	}
	return id
}

// actorOf is the audit actor for account user: "owner" or "user:<id>".
func actorOf(user string) string {
	if user == "" || user == accounts.OwnerID {
		return audit.ActorOwner
	}
	return "user:" + user
}

// actor is the audit actor of the signed-in account.
func actor(ctx context.Context) string {
	if sess := SessionFrom(ctx); sess != nil {
		return actorOf(sess.UserID)
	}
	return audit.ActorOwner
}

// errNoSession means a handler that needs an account ran without a session.
var errNoSession = errors.New("httpapi: no session")

// account loads the signed-in account; without a session it fails rather
// than falling back to the owner.
func (s *server) account(ctx context.Context) (accounts.Account, error) {
	sess := SessionFrom(ctx)
	if sess == nil {
		return accounts.Account{}, errNoSession
	}
	return accounts.Load(ctx, s.d.Pool, sess.UserID)
}

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
		"error": "locked", "message": "Too many failed attempts. This account is temporarily locked.",
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
	writeError(w, http.StatusUnauthorized, "password_required", "Sign in with your password first.")
}

// --- pending login state ---

type pendingLogin struct {
	token                         string
	passwordOK, emergency, device bool
	// user is the account whose password was checked; "" is the owner.
	user string
	// totpDue: the account has an authenticator code that is still to be
	// entered before anything else (passkey, session).
	totpDue bool
}

func loginKey(token string) string { return "login:" + token }

func (s *server) newPending(w http.ResponseWriter, r *http.Request, ctx context.Context, cfg settings.Settings, p pendingLogin) (pendingLogin, error) {
	tok, err := randomToken()
	if err != nil {
		return p, err
	}
	p.token = tok
	pipe := s.d.Valkey.TxPipeline()
	pipe.HSet(ctx, loginKey(tok), "password_ok", p.passwordOK, "emergency", p.emergency, "device", p.device, "user", p.user, "totp", p.totpDue)
	pipe.Expire(ctx, loginKey(tok), loginTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return p, err
	}
	setCookie(w, r, cfg, PendingCookie, loginKind+"."+tok, loginTTL)
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
	return pendingLogin{token: tok, passwordOK: h["password_ok"] == "1", emergency: h["emergency"] == "1", device: h["device"] == "1", user: h["user"], totpDue: h["totp"] == "1"}, true, nil
}

// consumePending deletes the pending state; false when it was already gone
// (used by a concurrent request).
func (s *server) consumePending(w http.ResponseWriter, r *http.Request, ctx context.Context, cfg settings.Settings, p pendingLogin) (bool, error) {
	n, err := s.d.Valkey.Del(ctx, loginKey(p.token)).Result()
	if err != nil {
		return false, err
	}
	setCookie(w, r, cfg, PendingCookie, "", 0)
	return n == 1, nil
}

// nextStep is the passkey step after a password: register when the account
// has none.
func (s *server) nextStep(ctx context.Context, user string) (string, error) {
	n, err := s.passkeyCount(ctx, user)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "register_passkey", nil
	}
	return "passkey", nil
}

// startPending creates a password_ok pending login for account user and
// answers {"next": …}.
func (s *server) startPending(w http.ResponseWriter, r *http.Request, ctx context.Context, cfg settings.Settings, user string, emergency bool) {
	next, err := s.nextStep(ctx, user)
	if err != nil {
		errInternal(w, "count passkeys", err)
		return
	}
	due, err := s.totpEnabled(ctx, user)
	if err != nil {
		errInternal(w, "load totp", err)
		return
	}
	if due {
		next = "totp"
	}
	if _, err := s.newPending(w, r, ctx, cfg, pendingLogin{passwordOK: true, emergency: emergency, user: user, totpDue: due}); err != nil {
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
	setCookie(w, r, cfg, SessionCookie, created.ID, time.Duration(cfg.Session.DurationHours)*time.Hour)
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
	setCookie(w, r, cfg, DeviceCookie, session.SignDevice(s.d.Secrets.SessionKey, id, fullAuth),
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
	// An account an admin added: its Keystone password, never AD.
	if u, err := accounts.ByUsername(ctx, s.d.Pool, account); err == nil && !u.IsOwner() {
		ok := crypto.VerifySecret(u.PasswordHash, req.Password)
		if !ok || u.Disabled {
			s.loginFailed(w, ctx, cfg, account, "password", ip)
			return
		}
		s.passwordAccepted(w, r, cfg, account, u.ID, ip)
		return
	} else if err != nil && !errors.Is(err, accounts.ErrNotFound) {
		errInternal(w, "load account", err)
		return
	}
	// An owner made in the short setup has a Keystone password: AD is not asked.
	var localHash string
	err := s.d.Pool.QueryRow(ctx, `select coalesce(password_hash, '') from owner where id = 1`).Scan(&localHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		errInternal(w, "load owner", err)
		return
	}
	if localHash != "" {
		owner, err := s.loadOwner(ctx)
		if err != nil {
			errInternal(w, "load owner", err)
			return
		}
		ok := crypto.VerifySecret(localHash, req.Password) // always run: no timing hint about the name
		if !ok || account != owner.Username {
			s.loginFailed(w, ctx, cfg, account, "password", ip)
			return
		}
		s.passwordAccepted(w, r, cfg, account, "", ip)
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
		writeError(w, http.StatusForbidden, "not_allowed", "This account is not allowed to sign in to Keystone.")
		return
	}
	s.passwordAccepted(w, r, cfg, account, "", ip)
}

// passwordAccepted continues a sign-in of account user ("" is the owner)
// whose password was right: on to the passkey, or straight in when no
// passkey is required.
func (s *server) passwordAccepted(w http.ResponseWriter, r *http.Request, cfg settings.Settings, account, user string, ip netip.Addr) {
	ctx := r.Context()
	if err := s.d.Limiter.ResetFailures(ctx, account); err != nil {
		slog.Error("auth: reset failures", "err", err)
	}
	due, err := s.totpEnabled(ctx, user)
	if err != nil {
		errInternal(w, "load totp", err)
		return
	}
	if cfg.Login.PasskeyRequired || due {
		s.startPending(w, r, ctx, cfg, user, false)
		return
	}
	now := time.Now()
	passkeys, err := s.passkeyCount(ctx, user)
	if err != nil {
		errInternal(w, "count passkeys", err)
		return
	}
	if !s.createSession(w, r, cfg, session.Session{UserID: user, LastFullAuthAt: now, NoPasskey: passkeys == 0}) {
		return
	}
	s.markDevice(w, r, cfg, now)
	s.d.Audit.Log(ctx, actorOf(user), "login.success", ip, map[string]any{"method": "password"})
	writeJSON(w, http.StatusOK, map[string]string{"next": "done"})
}

func (s *server) authPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	svc, err := s.passkeyService(r, cfg)
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
		opts, state, err = svc.BeginRegistration(ctx, sess.UserID)
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
		case ok && p.passwordOK && p.totpDue:
			writeError(w, http.StatusUnauthorized, "totp_required", "Enter the code from your authenticator app first.")
			return
		case ok && p.passwordOK:
			next, nerr := s.nextStep(ctx, p.user)
			if nerr != nil {
				errInternal(w, "count passkeys", nerr)
				return
			}
			bind = "login:" + p.token
			if next == "register_passkey" {
				kind = ceremonyRegister
				opts, state, err = svc.BeginRegistration(ctx, p.user)
			} else {
				kind = ceremonyLogin
				opts, state, err = svc.BeginLogin(ctx, p.user, false)
			}
		default:
			if _, _, trusted := s.trustedDevice(r, cfg); !trusted || !cfg.Login.PasskeyOnlyOnTrustedDevice {
				errPasswordRequired(w)
				return
			}
			if !ok || !p.device {
				if p, err = s.newPending(w, r, ctx, cfg, pendingLogin{device: true}); err != nil {
					slog.Error("auth: store pending", "err", err)
					errUnavailable(w)
					return
				}
			}
			bind, kind = "login:"+p.token, ceremonyLogin
			opts, state, err = svc.BeginLogin(ctx, "", true)
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
		writeError(w, http.StatusBadRequest, "no_ceremony", "Start the passkey step again.")
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
	writeError(w, http.StatusUnauthorized, "passkey_failed", "The passkey could not be verified. Try again.")
}

func (s *server) authPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)
	svc, err := s.passkeyService(r, cfg)
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
	if p.totpDue {
		writeError(w, http.StatusUnauthorized, "totp_required", "Enter the code from your authenticator app first.")
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
	user := p.user
	if kind == ceremonyRegister {
		method = "passkey_register"
		err = svc.FinishRegistration(ctx, p.user, state, r, passkeyName(r))
	} else {
		var signed string
		signed, err = svc.FinishLogin(ctx, state, r)
		switch {
		case err != nil:
		case p.passwordOK && signed != accountKey(p.user):
			err = errors.New("passkey of another account")
		case !p.passwordOK:
			user = accountRef(signed) // trusted device: whoever the passkey belongs to
		}
	}
	if err != nil {
		slog.Warn("auth: passkey", "kind", kind, "err", err)
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"method": method})
		errPasskeyFailed(w)
		return
	}
	if fresh, err := s.consumePending(w, r, ctx, cfg, p); err != nil || !fresh {
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
	if !s.createSession(w, r, cfg, session.Session{UserID: user, LastFullAuthAt: fullAuth, LastPasskeyAt: now, Emergency: p.emergency}) {
		return
	}
	if full {
		s.markDevice(w, r, cfg, now)
	}
	s.d.Audit.Log(ctx, actorOf(user), "login.success", ip, map[string]any{"method": method, "emergency": p.emergency})
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
	if err := svc.FinishRegistration(ctx, sess.UserID, state, r, name); err != nil {
		slog.Warn("auth: forced registration", "err", err)
		writeError(w, http.StatusBadRequest, "passkey_failed", "The passkey could not be registered. Try again.")
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
	s.d.Audit.Log(ctx, actorOf(sess.UserID), "passkey.registered", clientIP(ctx), map[string]any{"name": name, "after": "recovery"})
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
	if p.user != "" {
		writeError(w, http.StatusForbidden, "not_allowed", "Recovery codes are for the owner. Ask an admin to reset your password.")
		return
	}
	id, err := s.matchRecoveryCode(ctx, strings.ToUpper(strings.TrimSpace(req.Code)))
	if err != nil {
		errInternal(w, "recovery codes", err)
		return
	}
	invalid := func() {
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "login.failure", ip, map[string]any{"method": "recovery"})
		writeError(w, http.StatusUnauthorized, "invalid_code", "This recovery code is invalid or already used.")
	}
	if id == 0 {
		invalid()
		return
	}
	if fresh, err := s.consumePending(w, r, ctx, cfg, p); err != nil || !fresh {
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
		writeError(w, http.StatusForbidden, "emergency_disabled", "The emergency account is not turned on.")
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
			writeError(w, http.StatusForbidden, "ad_available", "Active Directory is reachable. Sign in with your normal password.")
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
	s.startPending(w, r, ctx, cfg, "", true)
}

func (s *server) stepUpBegin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	svc, err := s.passkeyService(r, settingsFrom(ctx))
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	opts, state, err := svc.BeginLogin(ctx, SessionFrom(ctx).UserID, false)
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
	svc, err := s.passkeyService(r, settingsFrom(ctx))
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if signed, err := svc.FinishLogin(ctx, state, r); err != nil || signed != accountKey(sess.UserID) {
		slog.Warn("auth: step-up", "err", err)
		s.d.Audit.Log(ctx, actorOf(sess.UserID), "login.failure", ip, map[string]any{"method": "stepup"})
		errPasskeyFailed(w)
		return
	}
	sess.LastPasskeyAt = time.Now()
	if err := s.d.Sessions.Update(ctx, sess); err != nil {
		slog.Error("auth: update session", "err", err)
		errUnavailable(w)
		return
	}
	s.d.Audit.Log(ctx, actorOf(sess.UserID), "stepup", ip, nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// stepUpPassword confirms a sensitive action with the account's password
// while it has no passkey (Keystone used over http, or set up without one).
// Once the account has a passkey, only the passkey confirms.
func (s *server) stepUpPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)
	sess := *SessionFrom(ctx)
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	passkeys, err := s.passkeyCount(ctx, sess.UserID)
	if err != nil {
		errInternal(w, "count passkeys", err)
		return
	}
	if passkeys > 0 {
		writeError(w, http.StatusForbidden, "passkey_required", "Confirm this with your passkey.")
		return
	}
	acct, err := s.account(ctx)
	if err != nil {
		errInternal(w, "load account", err)
		return
	}
	if !s.allow(w, ctx, "login-ip", ip.String(), cfg.BruteForce.LoginPerIPPerMinute) ||
		!s.allow(w, ctx, "login-account", acct.Username, cfg.BruteForce.LoginPerAccountPerMinute) ||
		!s.notLocked(w, ctx, acct.Username, ip) {
		return
	}
	ok, err := s.accountPassword(ctx, acct, req.Password)
	switch {
	case errors.Is(err, ad.ErrUnavailable):
		s.adUnavailable(w, ctx)
		return
	case err != nil:
		errInternal(w, "check password", err)
		return
	case !ok:
		s.loginFailed(w, ctx, cfg, acct.Username, "stepup", ip)
		return
	}
	// With an authenticator, the code is part of confirming.
	if on, err := s.totpEnabled(ctx, sess.UserID); err != nil {
		errInternal(w, "load totp", err)
		return
	} else if on {
		if req.Code == "" {
			writeError(w, http.StatusUnauthorized, "totp_required", "Also enter the code from your authenticator app.")
			return
		}
		good, err := s.checkTOTP(ctx, sess.UserID, req.Code, true)
		if err != nil {
			errInternal(w, "check code", err)
			return
		}
		if !good {
			s.loginFailed(w, ctx, cfg, acct.Username, "stepup", ip)
			return
		}
	}
	sess.NoPasskey, sess.LastFullAuthAt = true, time.Now()
	if err := s.d.Sessions.Update(ctx, sess); err != nil {
		slog.Error("auth: update session", "err", err)
		errUnavailable(w)
		return
	}
	s.d.Audit.Log(ctx, actorOf(sess.UserID), "stepup", ip, map[string]any{"method": "password"})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// accountPassword checks password against the account's Keystone password,
// or against AD for an owner without one.
func (s *server) accountPassword(ctx context.Context, acct accounts.Account, password string) (bool, error) {
	if acct.PasswordHash != "" {
		return crypto.VerifySecret(acct.PasswordHash, password), nil
	}
	if !acct.IsOwner() {
		return false, nil
	}
	dir := s.directory()
	if dir == nil {
		return false, ad.ErrUnavailable
	}
	actx, cancel := s.adContext(ctx)
	defer cancel()
	_, err := dir.Authenticate(actx, acct.Username, password)
	if errors.Is(err, ad.ErrInvalidCredentials) || errors.Is(err, ad.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
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
	setCookie(w, r, cfg, SessionCookie, "", 0)
	s.d.Audit.Log(ctx, actor(ctx), "logout", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	acct, err := s.account(ctx)
	if err != nil {
		errInternal(w, "load account", err)
		return
	}
	sess := SessionFrom(ctx)
	passkeys, err := s.passkeyCount(ctx, sess.UserID)
	if err != nil {
		errInternal(w, "count passkeys", err)
		return
	}
	g, err := s.grants(ctx)
	if err != nil {
		errInternal(w, "load grants", err)
		return
	}
	role := "member"
	switch {
	case acct.IsOwner():
		role = "owner"
	case g.Admin():
		role = "admin"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": acct.ID, "role": role, "admin": g.Admin(), "permissions": g.List(), "roles": g.Roles(), "app_admin_of": g.Apps(),
		"passkey_missing": passkeys == 0,
		"username":        acct.Username, "display_name": acct.DisplayName, "given_name": acct.GivenName, "email": acct.Email,
		"emergency": sess.Emergency, "must_register_passkey": sess.MustRegisterPasskey,
		"needs_stepup":            needsStepUp(*sess, settingsFrom(ctx), time.Now()),
		"clipboard_clear_seconds": settingsFrom(ctx).Vault.ClipboardClearSeconds,
		"client_ip":               clientIP(ctx).String(),
	})
}
