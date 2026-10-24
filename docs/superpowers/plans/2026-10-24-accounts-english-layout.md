# Accounts, English UI, Apple fixed-window layout

**Goal:** more people than the owner can sign in; the whole UI is English; the app is a fixed window that does not scroll as a page.

## 1. Accounts (backend)

- Migration 0007: `users` (id uuid, username unique, display_name, given_name, email, user_handle unique, password_hash, role `admin|member`, created_at, disabled_at). `passkeys.user_id` and `vault_items.user_id` (nullable; null = the owner). `apps.access` text[] (null = everyone; else account ids, `owner` for the owner).
- The owner stays in `owner` (AD link, recovery codes, emergency account) and is always admin. An *account* is the owner (id `owner`) or a `users` row.
- `internal/accounts`: `Account{ID, Username, DisplayName, GivenName, Email, UserHandle, PasswordHash, Role}`, `Load(ctx, q, id)`, `ByUsername`, `List`, `Create`, `Update`, `SetPassword`, `Delete`. `apps.Owner` becomes an alias of the account fields SAML/OIDC/LDAP map.
- Session gets `UserID` ("" = owner). Login: owner path unchanged; otherwise a local user by username + argon2. Same timing (VerifySecret always runs).
- Admin routes: `requireAdmin` (owner or role admin). Members: home, vault, own account (password, passkeys, sessions).
- Passkeys, sessions list, vault items: scoped to the session's account.
- SAML/OIDC/gate: subject = session account; refuse when the app's access excludes it. LDAP: bind as owner or user; app access applies.
- `/api/admin/users` CRUD (+ reset password); `/api/account/password` for the signed-in account; `/api/me` gains `id`, `role`.

## 2. Accounts (web)

- Instellingen › Users: list, add (username, name, email, password, role), edit, reset password, disable, delete.
- Apps sheet: "Who can use it": Everyone / chosen accounts.
- Nav: members see My apps, Vault, Account. Admins also see Settings.

## 3. English

- Every UI string, every API error message, audit labels, README stays Dutch? → README English too.
- Routes: /instellingen → /settings (+ /settings/branding, /settings/users …), /kluis → /vault; old paths redirect.

## 4. Apple fixed window

- Shell is 100dvh; sidebar and toolbar fixed; only the content pane scrolls when it must.
- Long panes (Security) split with a segmented control so each fits 1280×800 without scrolling.
- macOS System Settings proportions: 13px body in settings rows, 28px rows, grouped boxes radius 10, no shadows.
