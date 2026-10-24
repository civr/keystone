-- More accounts than the owner. The owner stays in `owner` (AD link,
-- recovery codes, emergency account) and is always an admin; everyone else
-- is a row here with a Keystone password. Passkeys and vault items belong to
-- an account: user_id null is the owner.
create table users (
  id uuid primary key default gen_random_uuid(),
  username text not null unique,
  display_name text not null default '',
  email text not null default '',
  user_handle bytea not null unique,
  password_hash text not null,
  role text not null check (role in ('admin', 'member')),
  created_at timestamptz not null default now(),
  disabled_at timestamptz
);

alter table passkeys add column user_id uuid references users (id) on delete cascade;
create index passkeys_user on passkeys (user_id);

alter table vault_items add column user_id uuid references users (id) on delete cascade;
create index vault_items_user on vault_items (user_id);

-- Who may use an app: null is everyone; otherwise account ids ('owner' for
-- the owner).
alter table apps add column access text[];
