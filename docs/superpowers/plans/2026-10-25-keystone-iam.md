# Keystone as an IAM platform

Two briefs (2026-10-25): a full redesign plus security upgrade, and Keystone as an
Authentik-class IAM platform with an Apple-grade experience. This plan merges them.
Order: identity and security foundation first, then the design system, then features.
Every visible control must work; anything not built is labelled "Not available yet".

## Analysis (what exists, what is reused)

Reused as is: SAML IdP (crewjam), OIDC provider (PKCE, JWKS, refresh), keystone-ldap,
keystone-gate (forward auth), passkeys (go-webauthn, per account), sessions in Valkey,
rate limits and lockout, audit log, branding, signing-key rotation, vault sealing
(AES-GCM with vault_key outside the database), app vault launch (form post).

Replaced or extended:
- Roles: today owner/admin/member with ad-hoc gates. → RBAC with permissions.
- App access: account ids only. → accounts and groups.
- One SPA with Settings for everyone. → separate Admin portal (/admin) and User portal.
- No MFA except passkeys. → TOTP as second factor.
- No external identity in front of Keystone. → trusted reverse-proxy header sign-in.
- Vault: built-in only. → provider architecture (built-in, 1Password Connect).

Known bug fixed first: the step-up password prompt stuck after a wrong password
(4c22285, with regression tests).

## Phase 1: RBAC and groups (backend)

- Permissions (strings) checked server-side on every route:
  `users.read users.write groups.write roles.write apps.read apps.write
   auth.write idp.write security.write sessions.manage audit.read
   vault.write branding.write system.write`.
- Built-in roles: Super Admin (all), Administrator (all but roles.write),
  Application Admin (apps.read, apps.write scoped to assigned apps),
  Security Admin (security.write, sessions.manage, audit.read, auth.write),
  Read-only Auditor (*.read, audit.read), User (none).
- Custom roles: name + permission set.
- Assignments to accounts or groups; app-scoped for Application Admin.
- The owner is always Super Admin. Nobody grants a permission they lack;
  only Super Admins grant Super Admin. Every change is audited.
- Groups with members; app access lists accept groups.
- Tests: member vs admin per route, escalation via direct API, scoped app admin.

## Phase 2: design system and portals

- Tokens for light and dark mode (prefers-color-scheme, plus a per-user choice).
- User portal: Okta-like launcher (white app cards with logos, Recently used,
  sections), My account, Security (passkeys, TOTP), Sessions, Access, Credentials,
  Preferences.
- Admin portal (/admin): authentik-like structure with a dark collapsible sidebar,
  Apple-grade finish: Dashboard (users, apps, sign-ins per day, recent events,
  most-used apps, warnings), Users & Groups, Applications, Authentication,
  Identity Providers, Access Policies, Vault & Secrets, Sessions & Security,
  Audit Logs, System.
- One component set (sheet, toast, confirm, forms, tables with search/filter).

## Phase 3: authentication

- TOTP enrolment, verification, recovery; policy per role (require MFA).
- Trusted reverse-proxy sign-in: identity header honoured only from trusted proxy
  CIDRs, mapped to an account by username or e-mail, optional auto-create.
- Per-app policy: allowed groups, required MFA, reauth window.

## Phase 4: vault and secrets

- Secret types: login, API key, token, client secret, note.
- Per-item policy: use only (never shown), or may be revealed.
- Lock: auto-lock after inactivity, manual lock; unlock with password or passkey.
- Provider interface: built-in (sealed with vault_key) and 1Password Connect
  (Connect URL + token sealed with vault_key; items read on demand, never cached).
- Security statement: anything Keystone fills in for you is decryptable by the server
  at that moment; items marked "use only" are still server-decryptable. True
  zero-knowledge is out of scope for auto-sign-in secrets and is stated as such.

## Phase 5: wizards, health and docs

- Guided forms for OIDC/SAML/LDAP/gate apps with validation and copyable values.
- Configuration health: missing https, no passkeys, public URL mismatch, AD down,
  weak settings, with next steps.
- Docs: install, admin, security model, integrations (IdPs, proxies, 1Password).
