package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/civr/keystone/internal/ad"
	"github.com/civr/keystone/internal/audit"
	"github.com/civr/keystone/internal/crypto"
	"github.com/civr/keystone/internal/session"
	"github.com/civr/keystone/internal/settings"
)

// Setup wizard. Progress is derived from stored data (no step column):
// domain = settings.domain + public_url set, ad = ad_config row, owner =
// owner row, passkey = at least one passkeys row, done = setup_state.completed_at.
// Steps run in order unlock → domain → ad → owner → passkey → (emergency) →
// finish; domain, ad and owner can be redone until the first passkey exists.

const (
	setupKind         = "setup" // ks_pending value prefix
	setupTTL          = 30 * time.Minute
	setupUnlockPerMin = 5
	emergencyMinLen   = 16
	recoveryCodeCount = 10
)

type setupProgress struct {
	done, domain, ad, owner, emergency bool
	passkeys                           int
	// passkeyLater: the owner chose to go on without a passkey (no https
	// yet); login.passkey_required is then off until one is added.
	passkeyLater bool
}

func (p setupProgress) passkeyDone() bool { return p.passkeys > 0 || p.passkeyLater }

// next is the step the wizard should show for an unlocked browser.
func (p setupProgress) next() string {
	switch {
	case p.done:
		return "done"
	case !p.domain:
		return "domain"
	case !p.ad:
		return "ad"
	case !p.owner:
		return "owner"
	case !p.passkeyDone():
		return "passkey"
	case !p.emergency:
		return "emergency"
	default:
		return "finish"
	}
}

func (s *server) setupProgress(ctx context.Context, cfg settings.Settings) (setupProgress, error) {
	p := setupProgress{domain: cfg.Domain != "" && cfg.PublicURL != "", passkeyLater: !cfg.Login.PasskeyRequired}
	err := s.d.Pool.QueryRow(ctx, `select
		coalesce((select completed_at is not null from setup_state where id = 1), false),
		exists(select 1 from ad_config where id = 1),
		exists(select 1 from owner where id = 1),
		(select count(*) from passkeys),
		exists(select 1 from emergency where id = 1 and enabled)`).
		Scan(&p.done, &p.ad, &p.owner, &p.passkeys, &p.emergency)
	return p, err
}

func setupKey(token string) string { return "setup:" + token }

// setupUnlocked reports whether r carries a live setup unlock token.
func (s *server) setupUnlocked(ctx context.Context, r *http.Request) (string, bool, error) {
	tok, ok := pendingToken(r, setupKind)
	if !ok {
		return "", false, nil
	}
	n, err := s.d.Valkey.Exists(ctx, setupKey(tok)).Result()
	return tok, n == 1, err
}

// Step preconditions; each returns "" when the step may run.
const (
	msgPrevious = "Rond eerst de vorige stap af."
	msgFrozen   = "Deze stap kan niet meer worden gewijzigd nadat er een passkey is geregistreerd."
)

func beforePasskey(prereq func(setupProgress) bool) func(setupProgress) string {
	return func(p setupProgress) string {
		if p.passkeys > 0 {
			return msgFrozen
		}
		if !prereq(p) {
			return msgPrevious
		}
		return ""
	}
}

var (
	condDomain  = beforePasskey(func(setupProgress) bool { return true })
	condAD      = beforePasskey(func(p setupProgress) bool { return p.domain })
	condOwner   = beforePasskey(func(p setupProgress) bool { return p.domain && p.ad })
	condADTest  = func(p setupProgress) string { return ifThen(!p.domain, msgPrevious) }
	condPasskey = func(p setupProgress) string { return ifThen(!(p.domain && p.ad && p.owner), msgPrevious) }
	condAfterPK = func(p setupProgress) string { return ifThen(!p.passkeyDone(), msgPrevious) }
)

func ifThen(c bool, msg string) string {
	if c {
		return msg
	}
	return ""
}

type setupHandler func(w http.ResponseWriter, r *http.Request, token string)

// setupStep guards a wizard step: 410 once setup is done, 401 without a live
// unlock, 409 when the step is out of order.
func (s *server) setupStep(cond func(setupProgress) string, h setupHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		p, err := s.setupProgress(ctx, settingsFrom(ctx))
		if err != nil {
			slog.Error("setup: progress", "err", err)
			errUnavailable(w)
			return
		}
		if p.done {
			errSetupDone(w)
			return
		}
		tok, ok, err := s.setupUnlocked(ctx, r)
		if err != nil {
			slog.Error("setup: unlock lookup", "err", err)
			errUnavailable(w)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "setup_locked", "Voer eerst de setupcode in.")
			return
		}
		if msg := cond(p); msg != "" {
			writeError(w, http.StatusConflict, "step_order", msg)
			return
		}
		h(w, r, tok)
	}
}

func errSetupDone(w http.ResponseWriter) {
	writeError(w, http.StatusGone, "setup_done", "De installatie is al afgerond.")
}

func errInternal(w http.ResponseWriter, what string, err error) {
	slog.Error("http: "+what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "Er ging iets mis. Probeer het opnieuw.")
}

func (s *server) setupRoutes(r chi.Router) {
	r.Get("/status", s.setupStatus)
	r.Post("/unlock", s.setupUnlock)
	r.Post("/domain", s.setupStep(condDomain, s.setupDomain))
	r.Post("/ad/test", s.setupStep(condADTest, s.setupADTest))
	r.Post("/ad", s.setupStep(condAD, s.setupAD))
	r.Post("/owner", s.setupStep(condOwner, s.setupOwner))
	r.Post("/passkey/begin", s.setupStep(condPasskey, s.setupPasskeyBegin))
	r.Post("/passkey/finish", s.setupStep(condPasskey, s.setupPasskeyFinish))
	r.Post("/emergency", s.setupStep(condAfterPK, s.setupEmergency))
	r.Post("/passkey/skip", s.setupStep(condPasskey, s.setupPasskeySkip))
	r.Post("/finish", s.setupStep(condAfterPK, s.setupFinish))
}

func (s *server) setupStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.setupProgress(ctx, settingsFrom(ctx))
	if err != nil {
		slog.Error("setup: progress", "err", err)
		errUnavailable(w)
		return
	}
	step := p.next()
	if !p.done {
		_, ok, err := s.setupUnlocked(ctx, r)
		if err != nil {
			slog.Error("setup: unlock lookup", "err", err)
			errUnavailable(w)
			return
		}
		if !ok {
			step = "unlock"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"needed": !p.done, "step": step})
}

func (s *server) setupUnlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	p, err := s.setupProgress(ctx, cfg)
	if err != nil {
		slog.Error("setup: progress", "err", err)
		errUnavailable(w)
		return
	}
	if p.done {
		errSetupDone(w)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if s.d.SetupCodeHash == "" {
		slog.Error("setup: no setup code generated")
		errUnavailable(w)
		return
	}
	// Reserve a hit before the (slow) verify so parallel guesses cannot all
	// slip past the limit, and refund it on success: only wrong codes count.
	ip := clientIP(ctx)
	allowed, err := s.d.Limiter.Allow(ctx, "setup-unlock", ip.String(), setupUnlockPerMin)
	if err != nil {
		slog.Error("setup: rate limit", "err", err)
		errUnavailable(w)
		return
	}
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Te veel pogingen. Wacht een minuut en probeer het opnieuw.")
		return
	}
	if !crypto.VerifySecret(s.d.SetupCodeHash, strings.ToUpper(strings.TrimSpace(req.Code))) {
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.unlock_failed", ip, nil)
		writeError(w, http.StatusUnauthorized, "bad_code", "De setupcode klopt niet.")
		return
	}
	if err := s.d.Limiter.Refund(ctx, "setup-unlock", ip.String()); err != nil {
		slog.Warn("setup: rate limit refund", "err", err)
	}
	tok, err := randomToken()
	if err != nil {
		errInternal(w, "token", err)
		return
	}
	pipe := s.d.Valkey.TxPipeline()
	pipe.HSet(ctx, setupKey(tok), "unlocked", 1)
	pipe.Expire(ctx, setupKey(tok), setupTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Error("setup: store unlock", "err", err)
		errUnavailable(w)
		return
	}
	// A browser still on the host address (http) after an https public_url
	// was saved would drop a Secure cookie and be stuck; Secure follows the
	// origin the request actually came from.
	ccfg := cfg
	if !originAllowed(r, cfg.PublicURL) {
		ccfg.PublicURL = ""
	}
	setCookie(w, ccfg, PendingCookie, setupKind+"."+tok, setupTTL)
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.unlocked", ip, nil)
	writeJSON(w, http.StatusOK, map[string]string{"step": p.next()})
}

// validHostname checks dot-separated labels of [a-z0-9-], 1–63 chars each.
func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, l := range strings.Split(h, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// normalizeDomain validates the domain step and returns the lowercase domain
// and public_url reduced to its origin (scheme://host[:port]).
func normalizeDomain(domain, publicURL string) (string, string, string) {
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if !validHostname(d) {
		return "", "", "Vul een geldig domein in, bijvoorbeeld home.lan."
	}
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", "", "Het publieke adres moet een http(s)-adres zonder pad zijn, bijvoorbeeld https://keystone.home.lan."
	}
	host := strings.ToLower(u.Hostname())
	if !validHostname(host) || (host != d && !strings.HasSuffix(host, "."+d)) {
		return "", "", "Het publieke adres moet binnen het domein " + d + " vallen."
	}
	return d, strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), ""
}

func (s *server) setupDomain(w http.ResponseWriter, r *http.Request, _ string) {
	ctx := r.Context()
	var req struct {
		Domain    string `json:"domain"`
		PublicURL string `json:"public_url"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	domain, publicURL, msg := normalizeDomain(req.Domain, req.PublicURL)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "invalid_domain", msg)
		return
	}
	cfg := settingsFrom(ctx)
	cfg.Domain, cfg.PublicURL = domain, publicURL
	if _, err := s.d.Settings.Put(ctx, cfg, clientIP(ctx)); err != nil {
		var ve *settings.ValidationError
		if errors.As(err, &ve) {
			writeError(w, http.StatusBadRequest, "invalid_domain", ve.Error())
			return
		}
		errInternal(w, "save settings", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.domain", clientIP(ctx), map[string]any{"domain": domain, "public_url": publicURL})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adRequest is an AD configuration with the bind password in plain text; it
// is sealed with data_key before use or storage.
type adRequest struct {
	ad.Config
	BindPassword string `json:"bind_password"`
}

func (s *server) readADConfig(w http.ResponseWriter, r *http.Request) (ad.Config, bool) {
	var req adRequest
	if !readJSON(w, r, &req) {
		return ad.Config{}, false
	}
	cfg := req.Config
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_ad_config", err.Error())
		return ad.Config{}, false
	}
	if req.BindPassword == "" {
		writeError(w, http.StatusBadRequest, "invalid_ad_config", "bind_password is verplicht")
		return ad.Config{}, false
	}
	sealed, err := crypto.Seal(s.d.Secrets.DataKey, []byte(req.BindPassword))
	if err != nil {
		errInternal(w, "seal bind password", err)
		return ad.Config{}, false
	}
	cfg.BindPasswordSealed = sealed
	return cfg, true
}

func (s *server) setupADTest(w http.ResponseWriter, r *http.Request, _ string) {
	cfg, ok := s.readADConfig(w, r)
	if !ok {
		return
	}
	test := s.d.TestAD
	if test == nil {
		test = func(ctx context.Context, c ad.Config) (int, error) { return ad.Test(ctx, c, s.d.Secrets.DataKey) }
	}
	n, err := test(r.Context(), cfg)
	res := map[string]any{"ok": err == nil, "error": "", "users_found": n}
	if err != nil {
		res["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *server) setupAD(w http.ResponseWriter, r *http.Request, _ string) {
	ctx := r.Context()
	cfg, ok := s.readADConfig(w, r)
	if !ok {
		return
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		errInternal(w, "encode ad config", err)
		return
	}
	// One statement: (re)saving AD also drops any owner verified against the
	// previous directory, so progress returns to the owner step. condAD
	// guarantees no passkey (bound to that owner) exists yet.
	if _, err := s.d.Pool.Exec(ctx, `with reset as (delete from owner)
		insert into ad_config (id, data) values (1, $1)
		on conflict (id) do update set data = excluded.data`, raw); err != nil {
		errInternal(w, "save ad config", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.ad", clientIP(ctx),
		map[string]any{"url": cfg.URL, "bind_dn": cfg.BindDN, "base_dn": cfg.BaseDN})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) setupOwner(w http.ResponseWriter, r *http.Request, _ string) {
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
	allowed, err := s.d.Limiter.Allow(ctx, "setup-owner", ip.String(), cfg.BruteForce.LoginPerIPPerMinute)
	if err != nil {
		slog.Error("setup: rate limit", "err", err)
		errUnavailable(w)
		return
	}
	if !allowed {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Te veel pogingen. Wacht een minuut en probeer het opnieuw.")
		return
	}
	var dir ad.Directory
	if s.d.Dir != nil {
		dir = s.d.Dir()
	}
	if dir == nil {
		writeError(w, http.StatusServiceUnavailable, "ad_unavailable", "AD is niet bereikbaar.")
		return
	}
	u, err := dir.Authenticate(ctx, req.Username, req.Password)
	switch {
	case errors.Is(err, ad.ErrInvalidCredentials), errors.Is(err, ad.ErrNotFound):
		s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.owner_failed", ip, map[string]any{"username": ad.NormalizeUsername(req.Username)})
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Gebruikersnaam of wachtwoord onjuist.")
		return
	case err != nil:
		slog.Warn("setup: AD authenticate", "err", err)
		writeError(w, http.StatusServiceUnavailable, "ad_unavailable", "AD is niet bereikbaar.")
		return
	}
	handle := make([]byte, 64)
	if _, err := rand.Read(handle); err != nil {
		errInternal(w, "user handle", err)
		return
	}
	// The user handle is kept on a redo: passkeys are bound to it.
	if _, err := s.d.Pool.Exec(ctx, `insert into owner (id, username, display_name, given_name, email, user_handle, created_at)
		values (1, $1, $2, $3, $4, $5, now())
		on conflict (id) do update set username = excluded.username, display_name = excluded.display_name,
			given_name = excluded.given_name, email = excluded.email`,
		ad.NormalizeUsername(u.Username), u.DisplayName, u.GivenName, u.Email, handle); err != nil {
		errInternal(w, "save owner", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.owner", ip, map[string]any{"username": ad.NormalizeUsername(u.Username)})
	writeJSON(w, http.StatusOK, map[string]any{"username": ad.NormalizeUsername(u.Username), "display_name": u.DisplayName})
}

func (s *server) setupPasskeyBegin(w http.ResponseWriter, r *http.Request, token string) {
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
	id, err := s.putCeremony(ctx, state)
	if err == nil {
		err = s.d.Valkey.HSet(ctx, setupKey(token), "wa", id).Err()
	}
	if err != nil {
		slog.Error("setup: store ceremony", "err", err)
		errUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *server) setupPasskeyFinish(w http.ResponseWriter, r *http.Request, token string) {
	ctx := r.Context()
	id, err := s.d.Valkey.HGet(ctx, setupKey(token), "wa").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		slog.Error("setup: load ceremony", "err", err)
		errUnavailable(w)
		return
	}
	state, err := s.takeCeremony(ctx, id)
	if errors.Is(err, errNoCeremony) {
		writeError(w, http.StatusBadRequest, "no_ceremony", "Start de registratie van de passkey opnieuw.")
		return
	}
	if err != nil {
		slog.Error("setup: take ceremony", "err", err)
		errUnavailable(w)
		return
	}
	_ = s.d.Valkey.HDel(ctx, setupKey(token), "wa").Err()
	svc, err := s.passkeyService(settingsFrom(ctx))
	if err != nil {
		errInternal(w, "passkey service", err)
		return
	}
	name := passkeyName(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := svc.FinishRegistration(ctx, state, r, name); err != nil {
		slog.Warn("setup: finish registration", "err", err)
		writeError(w, http.StatusBadRequest, "passkey_failed", "De passkey kon niet worden geregistreerd. Probeer het opnieuw.")
		return
	}
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.passkey", clientIP(ctx), map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) setupEmergency(w http.ResponseWriter, r *http.Request, _ string) {
	ctx := r.Context()
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if utf8.RuneCountInString(req.Password) < emergencyMinLen {
		writeError(w, http.StatusBadRequest, "weak_password", "Het noodwachtwoord moet minstens 16 tekens hebben.")
		return
	}
	hash, err := crypto.HashSecret(req.Password)
	if err != nil {
		errInternal(w, "hash emergency password", err)
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `insert into emergency (id, enabled, password_hash) values (1, true, $1)
		on conflict (id) do update set enabled = true, password_hash = excluded.password_hash`, hash); err != nil {
		errInternal(w, "save emergency", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.emergency", clientIP(ctx), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

var errAlreadyDone = errors.New("setup already completed")

// setupPasskeySkip goes on without a passkey: browsers only offer passkeys
// on https (or localhost), and the reverse proxy may not exist yet. Logins
// then use the AD password alone until a passkey is added in Instellingen.
func (s *server) setupPasskeySkip(w http.ResponseWriter, r *http.Request, _ string) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	cfg.Login.PasskeyRequired = false
	if _, err := s.d.Settings.Put(ctx, cfg, netip.Addr{}); err != nil {
		errInternal(w, "skip passkey", err)
		return
	}
	s.d.Audit.Log(ctx, audit.ActorAnonymous, "setup.passkey_skipped", clientIP(ctx), nil)
	p, err := s.setupProgress(ctx, cfg)
	if err != nil {
		errInternal(w, "setup progress", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"step": p.next()})
}

func (s *server) setupFinish(w http.ResponseWriter, r *http.Request, token string) {
	ctx := r.Context()
	cfg := settingsFrom(ctx)
	ip := clientIP(ctx)

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

	// Create the session first so a failure there never leaves a completed
	// setup whose recovery codes were not shown; revoke it if commit fails.
	now := time.Now()
	var passkeys int
	if err := s.d.Pool.QueryRow(ctx, `select count(*) from passkeys`).Scan(&passkeys); err != nil {
		errInternal(w, "count passkeys", err)
		return
	}
	sess := session.Session{LastFullAuthAt: now, IP: ip.String(), UserAgent: r.UserAgent(), NoPasskey: passkeys == 0}
	if passkeys > 0 {
		sess.LastPasskeyAt = now
	}
	sess, err := s.d.Sessions.Create(ctx, sess, cfg)
	if err != nil {
		slog.Error("setup: create session", "err", err)
		errUnavailable(w)
		return
	}
	if err := s.completeSetup(ctx, hashes); err != nil {
		_ = s.d.Sessions.Revoke(context.WithoutCancel(ctx), sess.ID)
		if errors.Is(err, errAlreadyDone) {
			errSetupDone(w)
			return
		}
		errInternal(w, "complete setup", err)
		return
	}
	_ = s.d.Valkey.Del(ctx, setupKey(token)).Err()
	setCookie(w, cfg, PendingCookie, "", 0)
	setCookie(w, cfg, SessionCookie, sess.ID, time.Duration(cfg.Session.DurationHours)*time.Hour)
	// The setup browser has just used AD (owner step) and a passkey.
	s.markDevice(w, r, cfg, now)
	s.d.Audit.Log(ctx, audit.ActorOwner, "setup.completed", ip, map[string]any{"recovery_codes": recoveryCodeCount})
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// completeSetup stores the recovery code hashes and sets completed_at in one
// transaction; a concurrent second finish gets errAlreadyDone.
func (s *server) completeSetup(ctx context.Context, hashes []string) error {
	tx, err := s.d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `insert into setup_state (id) values (1) on conflict (id) do nothing`); err != nil {
		return err
	}
	var completed *time.Time
	if err := tx.QueryRow(ctx, `select completed_at from setup_state where id = 1 for update`).Scan(&completed); err != nil {
		return err
	}
	if completed != nil {
		return errAlreadyDone
	}
	if _, err := tx.Exec(ctx, `delete from recovery_codes`); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, h := range hashes {
		batch.Queue(`insert into recovery_codes (hash) values ($1)`, h)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update setup_state set completed_at = now() where id = 1`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
