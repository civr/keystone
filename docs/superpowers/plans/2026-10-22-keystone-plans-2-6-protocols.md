# Keystone plans 2–6: protocols, apps and vault — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Connect Keystone to the five first apps: SAML IdP + Bastion (plan 2), OIDC provider + generic OIDC in Nimbus (plan 3), `keystone-ldap` + Jellyfin (plan 4), `keystone-gate` + Home Assistant auto-login (plan 5), vault + UniFi (plan 6).

**Architecture:** `keystone-core` (Go) gains signing keys, per-app protocol config with account mapping, the SAML IdP, the OIDC provider, gate-cookie issuance and the vault API. Two new Go binaries, `keystone-ldap` and `keystone-gate`, share Postgres (apps, settings) and Valkey (sessions, rate limits) with core. Bastion (`~/Projects/seamless`, Fastify/TS) gets a SAML SP; Nimbus (`~/Projects/proxmox-cloud`, FastAPI) gets generic OIDC. A Home Assistant custom component trusts the gate's user header.

**Tech Stack:** Go 1.27, chi, pgx, go-redis, `github.com/crewjam/saml` (IdP), `github.com/go-jose/go-jose/v4` (JWK/JWS), `github.com/jimlambrt/gldap` (LDAP server), go-ldap filter compile; SvelteKit 5 web; Bastion: `@node-saml/node-saml`; Nimbus: python-jose (already present); HA: Python custom component.

**Spec:** `docs/superpowers/specs/2026-10-19-keystone-design.md` (§2 protocols, §3 apps, §4, §5 vault, §7 containers + speed targets, §8, §10, §12).

## Global Constraints

- One user (the owner). Every protocol sends the **app account mapping** of that app (`username`, `email`, `display_name`), never the Keystone username unless the mapping is empty (then the owner's AD username/email/display name).
- Dutch UI copy and Dutch error `message`s; error shape `{"error":"<code>","message":"<Dutch>"}`.
- Writes to apps, keys, vault: session + step-up (`requireStepUp`), audit-logged.
- Audit actions added: `app.access` (detail `{app, protocol}`), `keys.rotated`, `vault.updated`, `vault.revealed`, `ldap.bind_failed`, `gate.denied`. Every new action gets a Dutch label in `web/src/lib/fixtures/audit-actions.json`.
- Secrets stay outside Postgres in clear: signing keys and LDAP/OIDC client secrets are stored sealed (`crypto.Seal` with `data_key`) or as argon2 hashes; vault passwords sealed with the new `vault_key` secret file (§5: key not in DB).
- Speed (§7): gate check < 5 ms p95 with session in Valkey (apps/settings cached ≤ 5 s in memory); LDAP bind < 50 ms p95 with warm AD (pooled conn reused per bind is not required — AD bind dominates).
- Images distroless/static for new Go binaries, same hardening as core (read_only, cap_drop ALL, no-new-privileges, uid 65532).
- No new required `.env` (§11): everything generated (keys lazily by core, `vault_key` by init).
- Cookie safety: `ks_session` stays host-only. The gate uses a separate `ks_gate` cookie on the parent domain whose token only authorizes gate checks.

## Review Focus

1. Open redirect / token leak: `next`, `rd`, SAML ACS and OIDC `redirect_uri` must be exact-match or same-domain checked — a crafted link must never send a code/assertion/cookie to a foreign host. (Tests in T3, T4, T6, T10.)
2. Logout and session revocation must kill downstream access: OIDC refresh tokens and gate tokens die with the Keystone session. (Tests in T6, T10/T11.)
3. Key rotation must not break signed-in apps instantly: JWKS keeps the previous OIDC key for 24 h; SAML metadata publishes old+new certificate until the next rotation. (Test in T1.)
4. LDAP: a service bind of app A must not search or bind users of app B; anonymous/empty-password binds always fail (empty password = unauthenticated bind in RFC 4513). (Tests in T8.)
5. Vault reveal outside the reconfirm window must answer `step_up_required`, never the password, and an app delete must delete its vault entry. (Tests in T13.)

---

## Part A — Keystone core

### Task 1: Settings additions + signing keys

**Files:**
- Modify: `internal/settings/settings.go`, `internal/settings/validate.go`, `internal/settings/settings_test.go`, `web/src/lib/fixtures/defaults.json` (regenerate via `keystone-core print-defaults`)
- Create: `internal/db/migrations/0003_keys_vault.sql`, `internal/keys/keys.go`, `internal/keys/keys_test.go`
- Modify: `internal/secrets/secrets.go` (+`vault_key` in `Files`, `Secrets.VaultKey []byte`), `internal/secrets/secrets_test.go`

**Interfaces:**
- Produces settings (JSON names exact): `tokens{access_minutes:10, id_minutes:10, refresh_days:30}`, `ldap{require_tls:true, allowed_cidrs:[], bind_per_ip_per_minute:10}`, `gate{fail_per_ip_per_minute:60}`, `vault{reconfirm_minutes:15, clipboard_clear_seconds:30, allow_reveal:true}`. Ranges: access/id 1–1440, refresh 0–365 (0 = no refresh tokens), bind 1–1000, gate 1–10000, reconfirm 1–1440, clipboard 5–600.
- Migration 0003: `signing_keys(id text primary key, use text not null check (use in ('saml','oidc','ldap')), key_sealed text not null, cert_pem text not null, created_at timestamptz not null, retired_at timestamptz)`; `vault_entries(app_id uuid primary key references apps(id) on delete cascade, username text not null default '', password_sealed text, updated_at timestamptz not null)`.
- Produces `package keys`: `type Key struct{ ID string; Use string; Signer *rsa.PrivateKey; Cert *x509.Certificate; CreatedAt time.Time; RetiredAt *time.Time }`; `func New(pool *pgxpool.Pool, dataKey []byte) *Store`; `(*Store).Active(ctx, use string) (Key, error)` — creates an RSA-2048 key + self-signed cert (CN "Keystone <use>", 10 years) when none is active, race-safe via `pg_advisory_xact_lock`; `(*Store).Published(ctx, use) ([]Key, error)` — active + keys retired < 24 h ago (oidc) / the single most recently retired (saml); `(*Store).Rotate(ctx, use) (Key, error)`. Key ID = base64url SHA-256 of DER public key, first 16 chars.

- [ ] Step 1: tests `TestDefaultsNewSections` (exact defaults above), `TestValidateNewRanges` (e.g. `vault.clipboard_clear_seconds=4` → field error), `TestEnsureCreatesVaultKey`, `TestActiveCreatesOnce` (two concurrent `Active` → same ID), `TestRotateKeepsPreviousPublished` (after Rotate, `Published("oidc")` has 2 keys, active first; with retired_at forced to now-25h → 1), `TestKeyIsSealed` (raw `key_sealed` column does not contain "PRIVATE KEY").
- [ ] Step 2: run `go test ./internal/settings ./internal/keys ./internal/secrets` → FAIL (undefined).
- [ ] Step 3: implement; sealed PEM via `crypto.Seal(dataKey, pem)`.
- [ ] Step 4: same command → PASS; regenerate `web/src/lib/fixtures/defaults.json`; `go test ./cmd/...` PASS.
- [ ] Step 5: commit `feat(core): token/ldap/gate/vault settings, signing key store`.

### Task 2: App protocols and account mapping

**Files:**
- Create: `internal/apps/apps.go` (types, validation, load), `internal/apps/apps_test.go`
- Modify: `internal/httpapi/apps.go`, `internal/httpapi/apps_test.go`, `internal/httpapi/routes.go`

**Interfaces:**
- Produces `package apps`:
  - `type Account struct{ Username, Email, DisplayName string }` (json `username,email,display_name`)
  - `type SAML struct{ EntityID, ACSURL string; IdPInitiated bool }` (json `entity_id, acs_url, idp_initiated`)
  - `type OIDC struct{ ClientID string; SecretHash string; RedirectURIs []string }` (json `client_id, secret_hash` (never sent to web), `redirect_uris`)
  - `type LDAP struct{ Slug string; BindSecretHash string }` (json `slug, bind_secret_hash` (never sent))
  - `type Gate struct{ Host string; BypassPaths []string }` (json `host, bypass_paths`; HA default `["/api/","/auth/token"]`, Jellyfin default `["/Users/AuthenticateByName","/socket","/emby/","/web/"]` are **suggestions in the web form**, not server defaults)
  - `type Config struct{ Account Account; SAML *SAML; OIDC *OIDC; LDAP *LDAP; Gate *Gate }`
  - `type App struct{ ID, Name, URL, Color, Icon, Protocol string; Config Config; Sort int }`
  - Protocols: `link, saml, oidc, ldap, gate, vault`.
  - `func (c *Config) Validate(protocol string) (field, msg string)` — saml: entity_id non-empty ≤ 1024, acs_url absolute https (http allowed only when host is an IP or ends in `.lan/.local/.home.arpa/.internal`); oidc: ≥1 redirect URI, each absolute, no fragment; ldap: slug `^[a-z0-9][a-z0-9-]{0,31}$`; gate: host lowercase hostname.
  - `func Load(ctx, q pgx-querier) ([]App, error)`; `func ByID(ctx, q, id) (App, error)`; `func (a App) Mapped(owner Owner) Account` (fills empty mapping fields from the owner).
  - `type Owner struct{ Username, DisplayName, Email string }`; `func LoadOwner(ctx, q) (Owner, error)`.
- HTTP: `appInput` gains `protocol` and `config`; server keeps `secret_hash`/`bind_secret_hash` across updates and generates `client_id` (uuid) on first save of an oidc app. Uniqueness: saml `entity_id`, oidc `client_id`, ldap `slug`, gate `host` unique across apps → 400 `invalid` field. `appOut` gains `config` (hashes stripped, `has_secret` bool instead).
- New routes (step-up): `POST /api/apps/{id}/secret` → for oidc/ldap apps generates a 32-byte base64url secret, stores its argon2 hash, returns `{"secret": "..."}` once.

- [ ] Step 1: tests `TestConfigValidate` (table: each protocol good/bad), `TestMappedFallsBackToOwner`, httpapi `TestCreateOIDCAppGetsClientID`, `TestSecretShownOnce` (GET never contains secret or hash), `TestDuplicateEntityIDRejected`, `TestUpdateKeepsSecretHash`.
- [ ] Step 2: `go test ./internal/apps ./internal/httpapi -run 'Config|Mapped|OIDCApp|Secret|Duplicate|KeepsSecret'` → FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: same → PASS; full `go test ./internal/httpapi` PASS.
- [ ] Step 5: commit `feat(core): per-app protocol config and account mapping`.

### Task 3: Sign-in continuation (`next`)

**Files:**
- Modify: `internal/httpapi/middleware.go` (+`requireSessionOrLogin`), `internal/httpapi/server_test.go`; `web/src/routes/login/+page.svelte`, `web/src/lib/nav.ts` (+`safeNext`), `web/src/lib/nav.test.ts`

**Interfaces:**
- Produces `func requireSessionOrLogin(next http.Handler) http.Handler` — no session → `302 /login?next=<url-escaped RequestURI>`; used by `/saml/*`, `/oidc/authorize`, `/api/gate/start`.
- Produces web `safeNext(raw: string|null): string` — returns raw only if it starts with one of `/saml/`, `/oidc/authorize`, `/api/gate/start` and not `//`, no `\`; else `/`. Login `finished()` uses `location.assign(safeNext(...))` for non-`/` targets (they are server routes, not SPA routes).

- [ ] Step 1: tests `TestRequireSessionOrLoginRedirects` (Location `/login?next=%2Foidc%2Fauthorize%3Fclient_id%3Dx`); vitest `safeNext` cases: `/oidc/authorize?x` ok, `//evil.com` → `/`, `https://evil` → `/`, `/instellingen` → `/`, `/\evil` → `/`.
- [ ] Step 2: run → FAIL. Step 3: implement. Step 4: `go test ./internal/httpapi -run Login && (cd web && npx vitest run src/lib/nav.test.ts)` → PASS.
- [ ] Step 5: commit `feat: continue to the requested app after sign-in`.

### Task 4: SAML 2.0 IdP

**Files:**
- Create: `internal/samlidp/idp.go`, `internal/samlidp/idp_test.go`, `internal/httpapi/saml.go`, `internal/httpapi/saml_test.go`
- Modify: `internal/httpapi/server.go` (mount), `internal/httpapi/middleware.go` (originCheck: exempt `POST /saml/sso`, which carries a SAMLRequest from a foreign SP)

**Interfaces:**
- Consumes: `keys.Store.Active/Published("saml")`, `apps.Load`, `apps.Mapped`, `requireSessionOrLogin`.
- Produces routes: `GET /saml/metadata` (IdP metadata: entityID = `<public_url>/saml/metadata`, SSO endpoint `<public_url>/saml/sso` Redirect+POST bindings, all published certs); `GET|POST /saml/sso` (SP-initiated, `requireSessionOrLogin` — for POST the request is first stashed in Valkey `samlreq:<tok>` 5 min and the user redirected to `GET /saml/sso?resume=<tok>` so the login redirect survives); `GET /saml/idp/{appID}` (IdP-initiated, only if `idp_initiated`).
- Response: signed Response + signed Assertion (RSA-SHA256), NameID format `unspecified` = mapped username, attributes `username`, `email`, `displayName`; Audience = SP entity ID; Destination/Recipient = ACS URL **from Keystone config only** (AuthnRequest's ACS URL must equal it, else 400); validity 5 min. Auto-POST HTML page with its own CSP: `default-src 'none'; script-src 'sha256-<hash of the submit script>'; form-action <ACS origin>; style-src 'unsafe-inline'`.
- Implementation choice: `crewjam/saml.IdentityProvider` with a `ServiceProviderProvider` built from apps and a `SessionProvider` that returns a `saml.Session` from the Keystone session; audit `app.access`.

- [ ] Step 1: tests `TestMetadataHasCert`, `TestSPInitiatedSignedResponse` (build AuthnRequest with crewjam `ServiceProvider` test SP, follow flow with a session cookie, parse POST form, `sp.ParseXMLResponse` succeeds, NameID == mapping), `TestUnknownSPRejected`, `TestACSMismatchRejected`, `TestNoSessionRedirectsToLogin`, `TestIdPInitiatedDisabledIs404`, `TestAutoPostCSPAllowsOnlyACS`.
- [ ] Step 2: run `go test ./internal/samlidp ./internal/httpapi -run SAML` → FAIL. Step 3: implement. Step 4: PASS.
- [ ] Step 5: commit `feat(core): SAML 2.0 identity provider`.

### Task 5: OIDC provider

**Files:**
- Create: `internal/oidc/provider.go`, `internal/oidc/provider_test.go`, `internal/httpapi/oidc.go`
- Modify: `internal/httpapi/server.go` (mount `/.well-known/openid-configuration`, `/oidc/*`), `internal/httpapi/middleware.go` (originCheck exempt `POST /oidc/token`; CORS not needed)

**Interfaces:**
- Consumes: `keys.Store` (`oidc`), `apps` (protocol oidc), `settings.Tokens`, `session.Store`.
- Discovery: issuer = `public_url` (no trailing slash); `authorization_endpoint` `/oidc/authorize`, `token_endpoint` `/oidc/token`, `userinfo_endpoint` `/oidc/userinfo`, `jwks_uri` `/oidc/jwks`, `end_session_endpoint` absent; `response_types_supported ["code"]`, `grant_types ["authorization_code","refresh_token"]`, `code_challenge_methods ["S256"]`, `id_token_signing_alg_values ["RS256"]`, `token_endpoint_auth_methods ["client_secret_basic","client_secret_post"]`, scopes `openid profile email offline_access`.
- Authorize (`requireSessionOrLogin`): `response_type=code`, `client_id` known, `redirect_uri` exact match of a registered URI (mismatch → 400 page, never redirect), `code_challenge` + `S256` **required**, `state` echoed, `nonce` stored. Code: 32 random bytes, Valkey `oidccode:<code>` 60 s, single use (`GETDEL`), bound to client, redirect_uri, challenge, nonce, session ID, scopes.
- Token: client auth by argon2 secret; `authorization_code` → `{access_token, token_type:"Bearer", expires_in, id_token, refresh_token?}`; refresh only with `offline_access` and `refresh_days>0`, stored Valkey `oidcrt:<hash>` bound to session ID, rotated on use. Code or refresh token whose session no longer exists → `invalid_grant`. Access token = signed JWT (aud = client_id, `scope`), lifetime `access_minutes`.
- ID token claims: `iss, sub` (= base64url owner user_handle, stable), `aud, exp, iat, auth_time` (session LastFullAuthAt), `nonce, preferred_username, email, name, amr:["pwd","hwk"]`.
- Userinfo: Bearer access token → `{sub, preferred_username, email, name}`.

- [ ] Step 1: tests `TestDiscovery`, `TestAuthorizeRedirectURIMismatchNoRedirect`, `TestAuthorizeRequiresPKCE`, `TestCodeFlowIDTokenVerifiesWithJWKS` (go-jose verify, claims == mapping, nonce echoed), `TestCodeSingleUse`, `TestWrongVerifier`, `TestRefreshRotatesAndDiesWithSession`, `TestUserinfo`, `TestTokenWrongSecret401`, `TestTokenOriginExempt` (POST without Origin reaches handler).
- [ ] Step 2: `go test ./internal/oidc ./internal/httpapi -run OIDC` → FAIL. Step 3: implement. Step 4: PASS.
- [ ] Step 5: commit `feat(core): OpenID Connect provider`.

### Task 6: Gate cookie issuance in core

**Files:**
- Create: `internal/gatetoken/gatetoken.go` (+test) — shared by core and gate
- Create: `internal/httpapi/gate.go`, `internal/httpapi/gate_test.go`; Modify: `internal/httpapi/auth.go` (logout also deletes the gate token)

**Interfaces:**
- Produces `package gatetoken`: `Cookie = "ks_gate"`; `func Issue(ctx, rdb, sessionID string, ttl time.Duration) (string, error)` (Valkey `gate:<sha256(tok)>` → sessionID); `func Resolve(ctx, rdb, sessions *session.Store, cfg settings.Settings, tok string, now time.Time) (session.Session, error)` (session must still be valid; uses `Get` so idle/expiry rules apply); `func RevokeSession(ctx, rdb, sessionID)` (index `gateidx:<sessionID>` set).
- Route `GET /api/gate/start?rd=<url>` (`requireSessionOrLogin`): `rd` absolute http(s), host == some gate app's `host`, and host is `settings.Domain` or a subdomain → set `ks_gate` (Domain = settings.Domain, Path /, HttpOnly, Secure if public_url https, SameSite Lax, Max-Age = session duration) and 302 to `rd`; else 400. Audit `app.access` `{protocol:"gate"}`.

- [ ] Step 1: tests `TestGateStartSetsDomainCookie`, `TestGateStartRejectsForeignHost` (`rd=https://evil.com`, `rd=https://home.lan.evil.com`), `TestResolveDiesWithSession`, `TestLogoutRevokesGateToken`.
- [ ] Step 2/3/4: `go test ./internal/gatetoken ./internal/httpapi -run Gate` FAIL → implement → PASS.
- [ ] Step 5: commit `feat(core): parent-domain gate cookie`.

### Task 7: Vault API

**Files:**
- Create: `internal/httpapi/vault.go`, `internal/httpapi/vault_test.go`; Modify: `internal/httpapi/routes.go`, `web/src/lib/fixtures/audit-actions.json`

**Interfaces:**
- Consumes: `Secrets.VaultKey`, migration 0003 `vault_entries`, `settings.Vault`.
- Routes: `GET /api/apps/{id}/vault` (session) → `{username, has_password}`; `PUT /api/apps/{id}/vault` (step-up) `{username, password?}` (omitted password keeps it; `""` clears) — only for protocol `vault`; `POST /api/apps/{id}/vault/password` (session) → `{password}` if `now - LastPasskeyAt ≤ reconfirm_minutes`, else 403 `step_up_required` (same code the web step-up hook handles). Audit `vault.updated` / `vault.revealed` (never the password).

- [ ] Step 1: tests `TestVaultRoundTrip`, `TestVaultPasswordNeedsRecentPasskey` (LastPasskeyAt = now-16 min → 403 step_up_required), `TestVaultSealedAtRest`, `TestVaultDeletedWithApp`, `TestVaultOnlyForVaultApps`.
- [ ] Step 2/3/4: `go test ./internal/httpapi -run Vault` FAIL → implement → PASS.
- [ ] Step 5: commit `feat(core): vault for app credentials`.

### Task 8: `keystone-ldap`

**Files:**
- Create: `cmd/keystone-ldap/main.go` (`serve`, `healthcheck`), `internal/ldapsrv/server.go`, `internal/ldapsrv/match.go`, `internal/ldapsrv/server_test.go`
- Modify: `cmd/keystone-core/main.go` — extract shared `openDB`/valkey/directory helpers into `internal/runtime/runtime.go` used by all three binaries.

**Interfaces:**
- Consumes: `apps.Load` (protocol ldap), `apps.Mapped`, `ad.Directory` (`Authenticate(owner.Username, password)`), `session.Limiter` (scope `ldap-ip`, `Fail`/`Locked` keyed `ldap:<owner>`), `keys.Store.Active("ldap")` for the TLS certificate, `settings.LDAP`.
- DIT: base `dc=keystone`; per app `ou=<slug>,dc=keystone`; service DN `cn=bind,ou=<slug>,dc=keystone` (password = app secret from T2); user entry `uid=<mapped username>,ou=<slug>,dc=keystone` with `objectClass: inetOrgPerson, uid, cn, displayName, mail, sn, givenName`, plus `memberOf: cn=users,ou=<slug>,dc=keystone`.
- Binds: empty password → invalidCredentials. Service bind checks argon2 secret. User bind: DN's `ou` = app, `uid` must equal the mapped username (case-insensitive) → `Authenticate`; AD unavailable → `unavailable` (52), no cached fallback (§9). Search allowed only after a service or user bind and only within that bind's `ou`; filters evaluated with `ldap.CompileFilter` + matcher over the single entry (equality, presence, substrings, and/or/not; case-insensitive).
- Listeners: LDAPS `:636` always (cert = ldap key/cert); plain `:389` only when `require_tls=false`; `allowed_cidrs` non-empty → others closed after accept. Audit `app.access {protocol:"ldap"}` on user bind success, `ldap.bind_failed` on failure.

- [ ] Step 1: tests (go-ldap client against an in-process server with `ad.Fake`): `TestServiceBindAndSearchFindsUser` (filter `(&(objectClass=inetOrgPerson)(uid=t.willems))`), `TestUserBindChecksAD`, `TestEmptyPasswordRejected`, `TestCrossAppSearchEmpty` (bound as app A, base `ou=b` → no entries / insufficientAccess), `TestUnboundSearchRejected`, `TestLockoutAfterThreshold`, `TestLDAPSOnlyWhenRequired`, `TestBindLatencyUnder50ms` (fake AD, p95 of 200 binds).
- [ ] Step 2: `go test ./internal/ldapsrv` → FAIL. Step 3: implement. Step 4: PASS; `go build ./cmd/...`.
- [ ] Step 5: commit `feat(ldap): keystone-ldap server for LDAP apps`.

### Task 9: `keystone-gate`

**Files:**
- Create: `cmd/keystone-gate/main.go` (`serve` on :8081, `healthcheck`), `internal/gate/gate.go`, `internal/gate/gate_test.go`

**Interfaces:**
- Consumes: `gatetoken.Resolve`, `apps.Load` (protocol gate) cached 5 s, settings cached 5 s, `session.Limiter` (scope `gate-ip`, `fail_per_ip_per_minute`).
- `GET /verify`: original request from `X-Forwarded-Proto/Host/Uri` (Traefik, Caddy) or `X-Original-URL` (nginx). Unknown host → 403. Path under a `bypass_paths` prefix → 200 without user headers. Valid `ks_gate` → 200 with `X-Keystone-User`, `X-Keystone-Email`, `X-Keystone-Name` (mapped account). Else → 302 to `<public_url>/api/gate/start?rd=<original URL>`; with `?mode=nginx` → 401 (nginx `error_page 401` does the redirect; the redirect target is `GET /login-redirect?rd=` on the gate which returns the same 302).
- Strips nothing itself; docs say the proxy must forward only `X-Keystone-*` from the gate response (`authResponseHeaders` / `copy_headers`).

- [ ] Step 1: tests `TestVerifyValidCookieSetsHeaders`, `TestVerifyNoCookieRedirects` (Location exact), `TestVerifyBypassPath`, `TestVerifyUnknownHost403`, `TestVerifyRevokedSession`, `TestVerifyNginxMode401`, `TestVerifyP95Under5ms` (miniredis-free: real Valkey via testutil, 500 calls).
- [ ] Step 2/3/4: `go test ./internal/gate` FAIL → implement → PASS.
- [ ] Step 5: commit `feat(gate): forward-auth service`.

### Task 10: Deploy: compose, images, CI, docs

**Files:**
- Modify: `docker-compose.yml` (+`ldap` service publishing `${KEYSTONE_LDAPS_PORT:-636}:636`, +`gate` service publishing `${KEYSTONE_GATE_PORT:-8081}:8081`, both depend on core healthy), `docker/core.Dockerfile` → build all three binaries; Create `docker/ldap.Dockerfile`, `docker/gate.Dockerfile`; `.github/workflows/release.yml` (matrix + 2 images), `.github/workflows/ci.yml` if it lists images; `scripts/smoke.sh` (gate `/verify` without cookie → 302, LDAPS port open); `README.md` sections: Bastion (SAML), Nimbus (OIDC), Jellyfin (LDAP plugin settings), Home Assistant (gate + component), UniFi (vault), plus Traefik/Caddy/nginx forward-auth snippets.

- [ ] Step 1: `docker compose config -q` and `actionlint` pass; `scripts/smoke.sh` passes against `docker compose up --build` (if Docker is healthy; else ruling).
- [ ] Step 2: commit `feat(deploy): ldap and gate containers, app setup docs`.

### Task 11: Web: apps editor per protocol, vault tile, new settings, key rotation

**Files:**
- Modify: `web/src/lib/apps.ts` (types `AppConfig`), `web/src/lib/appEdit.ts` (+validation per protocol, mirrors T2), `web/src/routes/instellingen/apps/+page.svelte` (protocol picker with Dutch labels: Link, SAML, OpenID Connect, LDAP, Poortwachter, Kluis; per-protocol fields; account mapping fields; copyable URLs: SAML metadata `<public_url>/saml/metadata`, OIDC issuer, LDAP base/bind DN, gate verify URL; "Nieuw geheim" button showing the secret once; vault username/password), `web/src/lib/components/AppTile.svelte` (vault: click opens URL in new tab + copies password, toast "Wachtwoord gekopieerd — wordt over 30 s gewist", clears clipboard after `clipboard_clear_seconds` if unchanged; username copy button), `web/src/lib/settingsMeta.ts` (+tokens/ldap/gate/vault sections with Dutch help and weakening warnings: `ldap.require_tls=false`, `vault.allow_reveal=true` not weakening), `web/src/routes/instellingen/beveiliging/+page.svelte` (+"Sleutels" card: rotate SAML/OIDC/LDAP keys with confirm), `internal/httpapi/routes.go` + `admin_security.go` (`GET /api/admin/keys`, `POST /api/admin/keys/{use}/rotate` step-up, audit `keys.rotated`).
- Test: `appEdit.test.ts`, `apps.test.ts` (tile action per protocol), `settingsMeta.test.ts`, `vaultClipboard.test.ts` (new `web/src/lib/vaultClipboard.ts`: `copyThenClear(text, seconds, clipboard, timers)`), Go `TestRotateKeyEndpoint`.

- [ ] Step 1: write tests; Step 2: `cd web && npx vitest run` FAIL; Step 3: implement; Step 4: `npx vitest run && npm run check && npm run build` PASS (bundle check script stays green), `go test ./internal/httpapi -run Rotate` PASS.
- [ ] Step 5: commit `feat(web): protocol app editor, vault tiles, key rotation`.

### Task 12: Home Assistant component

**Files:**
- Create: `integrations/home-assistant/custom_components/keystone_auth/{__init__.py,manifest.json,headers.py}`, `integrations/home-assistant/tests/test_headers.py`, `integrations/home-assistant/README.md`

**Interfaces:**
- `headers.py` (no HA import): `def trusted(remote: str, proxies: list[str]) -> bool`; `def user_from(headers: Mapping[str,str], header: str="X-Keystone-User") -> str | None` (strip, lowercase, max 64, `[a-z0-9._@-]` only).
- `__init__.py`: config `keystone_auth: {header: X-Keystone-User, trusted_proxies: [...]}`; registers an auth provider (like hass-auth-header) whose login flow, when the request comes from a trusted proxy with a valid header, finishes immediately for the HA user whose username equals the header; otherwise falls through to the normal login form.

- [ ] Step 1: tests for `trusted` (CIDR, IPv6, untrusted) and `user_from` (missing, bad chars, case). Step 2: `python3 -m pytest integrations/home-assistant/tests` FAIL. Step 3: implement. Step 4: PASS. Step 5: commit `feat(ha): keystone_auth custom component`.

## Part B — Bastion (`~/Projects/seamless`, branch `feat/keystone-saml`)

### Task 13: SAML SP sign-in

**Files:** Create `api/src/auth/saml.ts`, `api/src/auth/samlRoutes.ts`, `api/src/auth/saml.test.ts`; Modify `api/src/auth/authConfig.ts` (+`samlEnabled`, `samlIdpMetadataUrl`, `samlIdpMetadataXml`, `samlAdminGroup` (AD group DN)), `api/src/db/migrations.ts`, route registration, admin UI auth settings page (SAML card: metadata URL + "Vernieuwen", SP metadata URL to copy), sign-in page button "Inloggen met Keystone".

- Flow: `GET /api/auth/saml/start` → node-saml AuthnRequest (Redirect binding) to IdP SSO; `POST /api/auth/saml/acs` → `validatePostResponseAsync` with `wantAssertionsSigned: true`, `audience = <publicUrl>/api/auth/saml/metadata`, InResponseTo checked (in-memory cache, 10 min); username = NameID; access via existing LDAP directory group lookup for that username when LDAP is configured (`accessFromGroups` with AD group DNs), else no environments; admin = member of `samlAdminGroup`; `startSession(... kind: "sso" ...)` so the existing one-time AD password flow for the RDS host applies. `GET /api/auth/saml/metadata` SP metadata.
- Tests: `saml start redirects to IdP`, `acs with locally signed response signs in`, `acs with unsigned/wrong-cert response refused`, `acs replay refused`, `wrong audience refused`. Run `npm test` in `api/`. Commit `feat(auth): sign in with Keystone over SAML`.

## Part C — Nimbus (`~/Projects/proxmox-cloud`, branch `feat/generic-oidc`)

### Task 14: Generic OIDC

**Files:** Create `backend/app/oidc.py`, `backend/tests/test_oidc.py`; Modify `backend/app/routers/auth.py` (SSO via `oidc.py` for providers `oidc` and `azure` — azure becomes issuer `https://login.microsoftonline.com/<tenant>/v2.0`), `backend/app/routers/admin.py` (accept `sso_provider in ('azure','oidc')`, `sso_issuer_url` https required for oidc), `frontend/src/pages/Settings.jsx` (provider choice + issuer field), i18n strings, Login button label from provider ("Inloggen met Keystone" when oidc).

- `oidc.py`: `discover(issuer) -> dict` (cached 1 h, issuer must match), `authorize_url(cfg, redirect_uri, state, nonce, challenge) -> str` (no `prompt` for oidc), `exchange(cfg, code, redirect_uri, verifier) -> dict`, `verify_id_token(cfg, id_token, nonce) -> dict` (JWKS via python-jose, alg RS256/ES256, iss/aud/exp/nonce checked). PKCE S256; state JWT carries nonce + verifier (signed with app secret, 10 min).
- User match: `preferred_username` == `User.username` (case-insensitive) first, then `email`; create as today otherwise.
- Tests (pytest, requests mocked): `test_discovery_issuer_mismatch`, `test_verify_ok`, `test_verify_bad_signature`, `test_verify_wrong_nonce`, `test_verify_wrong_aud`, `test_authorize_url_has_pkce`. Commit `feat(sso): generic OpenID Connect (Keystone)`.

---

## Execution order

T1 → T2 → T3 → T4 → T5 → T6 → T7 → T8 → T9 → T11 → T10 → T12 → T13 → T14, then one final review (opus) over Keystone + both app branches.
