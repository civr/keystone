-- Signing keys for SAML, OIDC and LDAPS, sealed with data_key. The active key
-- of a use has retired_at null; retired keys stay published for a while.
create table signing_keys (
  id text primary key,
  use text not null check (use in ('saml', 'oidc', 'ldap')),
  key_sealed text not null,
  cert_pem text not null,
  created_at timestamptz not null,
  retired_at timestamptz
);
create unique index signing_keys_active on signing_keys (use) where retired_at is null;

-- Vault credentials per app; the password is sealed with vault_key.
create table vault_entries (
  app_id uuid primary key references apps (id) on delete cascade,
  username text not null default '',
  password_sealed text,
  updated_at timestamptz not null
);
