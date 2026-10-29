# Keystone

Self-hosted identity provider (SSO) for your home network, with optional Active Directory as a source.

## Install

```sh
git clone <repo-url> keystone && cd keystone
docker compose pull && docker compose up -d
docker compose logs core | grep "setup code"
```

Then open `http://<host>:8080`, enter the setup code from the logs and create an account (username and
password). That is all setup needs. Everything else (domain, Active Directory, passkeys, emergency account)
comes later in **Settings**. Active Directory is optional: the owner can sign in with the Keystone password.
To pick another port, use `KEYSTONE_PORT=9000 docker compose up -d`.

The images come ready-made from `ghcr.io/civr/keystone-*`; the server compiles nothing. Building from source
(only needed if you change the code) uses the extra file `docker-compose.build.yml`, see Development.

On first start the `init` container generates the secrets (database and valkey passwords, session, data and
vault keys) in the `keystone_secrets` volume. Existing secrets are never overwritten.

## People, vault and branding

- **Users:** admins can add more people under **Settings › Users** (admin or member). Each app can be
  limited to chosen people.
- **Vault:** a page for any passwords, private per account. Apps with the vault protocol also appear as
  tiles (see UniFi OS below).
- **Branding:** set the name, logo, accent colour and background colour under **Settings › Branding**.

## Admin portal

Everyone signs in to the user portal (My apps, Vault, Account). People with a role also get the admin portal at
`/admin`: a dashboard, Directory (users, groups, roles), Applications (apps with templates for Jellyfin, Home
Assistant, Grafana, Proxmox and more), Authentication (sign-in and MFA, reverse-proxy sign-in, identity providers,
sessions), Security (vault and 1Password Connect, network, keys and recovery, audit log) and System.

Built-in roles: Super Admin, Administrator, Application Admin (per app), Security Admin and Read-only Auditor; add your
own under Roles. See [docs/security.md](docs/security.md) for what Keystone guarantees and what it does not.

## Reverse proxy

Keystone works without a reverse proxy: just use `http://<ip>:8080`. Passkeys need https, though, so for
passkeys put a reverse proxy in front of it. Keystone does not speak TLS itself; end TLS on the proxy, for
example with `keystone.<domain>` pointing to port 8080 of this host. The proxy must pass on
`X-Forwarded-For`. Port 8080 of the `core` container is deliberately never published.

Example (Caddy on the proxy host):

```
keystone.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

Once you are signed in through the proxy, open **Settings › General** and choose **Use this address**. That
makes the address you are on the public one.

### Setting trusted proxies

Keystone only believes `X-Forwarded-For` from its own `web` container and from addresses you list as
trusted proxies. Without that setting, Keystone sees every visitor as the address of your reverse proxy:
rate limits, blocks and the allowed IP ranges then apply to everyone at once.

1. Sign in through the reverse proxy and open **Settings › Security › Network**. It shows
   *Your address as Keystone sees it* (you see the same address for your session under
   **Settings › Devices**).
2. If that is the address of the proxy instead of your own device, add it to **Trusted proxies**.

If the proxy runs on the same host and connects to the published port, it usually arrives through the
Docker gateway of the compose network, for example `172.18.0.1`. Find it with:

```sh
docker network inspect keystone_default --format '{{(index .IPAM.Config 0).Gateway}}'
```

and add `172.18.0.1/32`. If the proxy runs on another machine, use that machine's IP address. After saving,
*Your address as Keystone sees it* shows your real address.

If you set a wrong public address by mistake, fix it under **Settings › General**. Passkeys only work over
https (or `localhost`).

If you want the bundled `web` container to fetch certificates itself, remove `auto_https off` from
`deploy/Caddyfile`, replace `:80` with your hostname and publish port 443 too (and give Caddy a persistent
volume for `/data`).

## Connecting apps

Add an app under **Settings › Apps** and choose how Keystone signs in. After saving, the dialog shows the
details the app needs, with a copy button. Under *Owner’s account in the app* you enter who Keystone signs you in
as for that app (empty = your own account). You can limit each app to chosen people.

### Bastion (SAML)

In Keystone: protocol **SAML**, entity ID `https://bastion.<domain>/api/auth/saml/metadata`, ACS URL
`https://bastion.<domain>/api/auth/saml/acs`, tile address `https://bastion.<domain>/api/auth/saml/start`.
In Bastion: turn on *Admin › Sign-in › Keystone (SAML)*, metadata URL `https://keystone.<domain>/saml/metadata`.
After the first SAML login, Bastion asks once for your AD password for the session host.

### Project Nimbus (OpenID Connect)

In Keystone: protocol **OpenID Connect**, redirect URI `https://nimbus.<domain>/api/auth/sso/callback`,
tile address `https://nimbus.<domain>/api/auth/sso/redirect`; then create a client secret.
In Nimbus: *Settings › SSO*, provider **OpenID Connect**, issuer `https://keystone.<domain>`, plus the
client ID and secret from Keystone. PKCE is required; Nimbus uses it automatically.

### Jellyfin (LDAP)

In Keystone: protocol **LDAP**, name `jellyfin`, create a bind password. In the Jellyfin LDAP plugin:

| Setting | Value |
|---|---|
| LDAP Server | the Keystone host, port `636`, *Secure LDAP* on |
| Bind User | `cn=bind,ou=jellyfin,dc=keystone` + the bind password |
| Base DN | `ou=jellyfin,dc=keystone` |
| User Search Filter | `(objectClass=inetOrgPerson)` |
| Login attribute / filter | `uid` / `(uid={username})` |

keystone-ldap has its own self-signed certificate (download it under *Settings › Security › Keys & recovery*).
Trust it in Jellyfin or turn on *Skip SSL verification*. Your password is checked on every login (against
AD if you use it); if AD is unreachable, signing in to Jellyfin stops working for a while.

### Home Assistant (gate)

In Keystone: protocol **Gate**, hostname `ha.<domain>`, exceptions via the *Home Assistant* suggestion
(the phone app uses its own tokens). Your reverse proxy asks `keystone-gate` (port 8081) on every request
whether it is allowed. The gate cookie applies to the whole domain you set in **Settings › General**, so
Keystone and the apps must live under it.

Traefik:

```yaml
http:
  middlewares:
    keystone:
      forwardAuth:
        address: http://keystone-host:8081/verify
        authResponseHeaders: [X-Keystone-User, X-Keystone-Email, X-Keystone-Name]
```

Caddy:

```
ha.example.com {
	request_header -X-Keystone-*
	forward_auth keystone-host:8081 {
		uri /verify
		copy_headers X-Keystone-User X-Keystone-Email X-Keystone-Name
	}
	reverse_proxy homeassistant:8123
}
```

nginx:

```
location = /_keystone { internal; proxy_pass http://keystone-host:8081/verify?mode=nginx;
  proxy_set_header X-Original-URL $scheme://$http_host$request_uri; proxy_pass_request_body off; proxy_set_header Content-Length ""; }
location @keystone_login { proxy_pass http://keystone-host:8081/login-redirect?rd=$scheme://$http_host$request_uri; }
location / { auth_request /_keystone; error_page 401 = @keystone_login;
  auth_request_set $ks_user $upstream_http_x_keystone_user;
  auth_request_set $ks_email $upstream_http_x_keystone_email;
  auth_request_set $ks_name $upstream_http_x_keystone_name;
  proxy_set_header X-Keystone-User $ks_user;
  proxy_set_header X-Keystone-Email $ks_email;
  proxy_set_header X-Keystone-Name $ks_name;
  proxy_pass http://homeassistant:8123; }
```

All three `proxy_set_header` lines are needed: this way nginx replaces whatever a visitor sends itself, and
drops the header when the gate does not set it (for an exception).

Let only your reverse proxy reach port 8081 and make sure it never passes on `X-Keystone-*` from visitors
(Traefik replaces them automatically; with Caddy, `request_header -X-Keystone-*` does that). For automatic
sign-in to Home Assistant itself, install the component in `integrations/home-assistant` (see the README
there).

### UniFi OS (vault)

Protocol **Vault**, with the username and password of your UniFi account. Clicking the tile copies the
password (after a passkey confirmation if the last one was more than 15 minutes ago, or after confirming
with your password if you have no passkey) and opens UniFi; the clipboard is cleared after 30 seconds. The
password is stored encrypted with `vault_key` from the secrets volume, not readable in the database.

## Update

```sh
git pull
docker compose pull && docker compose up -d
```

The images are built on GitHub on every push to `main`; the server only downloads them.

## Locked out? Reset the network restrictions

If you locked yourself out with the network restrictions (allowed IP ranges), clear them with:

```sh
docker compose exec core keystone-core reset-network
```

## Backup

Back up the two volumes: `keystone_db` (Postgres) and `keystone_secrets` (keys and passwords). Without
`keystone_secrets` the database can no longer be decrypted; keep them separately and safely.

The `keystone_valkey` volume only holds sessions and rate-limit counters. A backup is not needed: if you
lose it, you just sign in again.

## Security notes

- The valkey password is not in any environment variable, but it is in the command line of
  `valkey-server` inside the valkey container (`--requirepass`). Anyone who can view processes in that
  container (or may use `docker top` on the host) can read it. Valkey is only reachable inside the compose
  network and only holds sessions; so do not give others Docker access on the host.

## Development

`make test`, `make web-test` and `bash scripts/smoke.sh` (full compose smoke test, cleans up after itself).

Build from source instead of downloading:

```sh
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

BuildKit keeps the Go and npm caches between builds; only the first build takes long. Use the same
`KEYSTONE_VERSION` for `core` and `web` (build arg, default `dev`; this file handles that). `core` holds the
start page that points to the files in `web`; with different values the page stays empty.
