-- One-time codes from an authenticator app, per account ('owner' or a
-- users.id). The secret is sealed with data_key; last_step stops a code from
-- being used twice. confirmed_at is null until the first code is entered.
create table totp (
  account_id text primary key,
  secret_sealed text not null,
  confirmed_at timestamptz,
  last_step bigint not null default 0,
  created_at timestamptz not null default now()
);
