# Keystone security model

What Keystone guarantees, what depends on your configuration, and what it cannot do.

## Accounts and access

- **Authorization is checked on the server for every request.** Hiding a menu item is a convenience only.
- **Roles are sets of permissions** (`users.read`, `users.write`, `groups.write`, `roles.write`, `apps.read`,
  `apps.write`, `auth.write`, `idp.write`, `security.write`, `sessions.manage`, `audit.read`, `vault.write`,
  `branding.write`, `system.write`). They are given to people or groups, globally or for one app.
- **No escalation.** You can give or take away a role only if you hold all its permissions; only super admins make
  super admins; built-in roles are fixed; adding someone to a group that carries roles counts as giving them; you
  cannot reset the password of, disable, delete or sign out an account with rights you lack.
- **The owner** (made during setup) is always a super admin and keeps the recovery codes, the emergency account and
  the Active Directory link.
- **Deleted usernames are never reused**, so a newcomer does not inherit an old identity at an app. E-mail addresses
  are unique for the same reason.

## Sign-in

- Passwords are stored as argon2 hashes. Failed attempts are rate limited per address and per account, and lead to a
  temporary lock.
- **Second factors:** passkeys (WebAuthn) and authenticator codes (TOTP, RFC 6238, each code usable once, secret
  sealed with `data_key`). Apps can require that a second factor was used.
- **Sensitive actions** ask you to confirm again (passkey, or password plus code while you have no passkey).
- **Reverse-proxy sign-in** trusts the identity header only when the request reached Keystone directly from a
  configured proxy address (the last `X-Forwarded-For` hop, which Keystone's own Caddy appends). Your proxy must strip
  that header from client requests. Sign-in through the proxy skips Keystone's own passkey and code steps.
- Without https, passkeys do not work and cookies are not marked Secure. Run Keystone behind https.

## Vault

- Vault passwords, notes and provider tokens are sealed with AES-GCM using `vault_key`, a file in the secrets volume,
  outside the database. A database dump alone does not reveal them.
- Passwords are never shown in the interface, only copied or used to sign in for you.
- **Limits you should know:** anything Keystone fills in or copies for you, the server can decrypt at that moment.
  Someone with both the database and the secrets volume (or control of the running server) can read every vault
  secret. This is not a zero-knowledge vault.
- **1Password Connect:** linked items store only a reference; the secret is read from Connect when used and not
  stored or cached. The Connect token is sealed with `vault_key`; its reach is what you allowed in 1Password, so give
  it only the vaults Keystone needs.
- Shared app passwords (vault tiles) are owner-only until an admin chooses who may use them.

## Apps

- SAML responses and OIDC tokens are signed with keys stored sealed in the database; keys can be rotated.
- OIDC requires PKCE (S256) and checks redirect URIs exactly.
- "Sign in for me" posts the app's login form from a one-time page whose Content-Security-Policy allows the form to go
  only to the configured login address.
- LDAP apps see only the people allowed to use them; LDAPS is required unless you turn plain LDAP on.

## Audit

Every sign-in, failure, change of settings, roles, groups, accounts, apps and vault use is recorded without secret
values. Retention and syslog forwarding are under Admin › Audit log.
