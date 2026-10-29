-- Vault 2: a kind per item (login, API key, token, client secret, note) and
-- credential providers. An item from a provider (1Password Connect) keeps
-- only a reference; the secret is fetched when used and never stored.
alter table vault_items add column kind text not null default 'login'
  check (kind in ('login', 'api_key', 'token', 'client_secret', 'note'));

create table vault_providers (
  id uuid primary key default gen_random_uuid(),
  kind text not null check (kind in ('onepassword')),
  name text not null unique,
  url text not null,
  token_sealed text not null,
  -- who may link items from it: null is admins only; else account ids and
  -- 'group:<id>' entries.
  access text[],
  created_at timestamptz not null default now()
);

alter table vault_items add column provider_id uuid references vault_providers (id) on delete restrict;
alter table vault_items add column provider_ref text not null default '';
