-- The vault's own items (Kluis): sign-in details for anything, not tied to
-- an app tile. Password and note are sealed with vault_key.
create table vault_items (
  id uuid primary key default gen_random_uuid(),
  title text not null,
  url text not null default '',
  username text not null default '',
  password_sealed text,
  note_sealed text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index vault_items_title on vault_items (lower(title));
