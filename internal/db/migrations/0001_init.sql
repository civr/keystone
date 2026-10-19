create table settings (
  id int primary key check (id = 1),
  data jsonb not null,
  updated_at timestamptz not null
);

create table ad_config (
  id int primary key check (id = 1),
  data jsonb not null -- bind password sealed with data_key
);

create table owner (
  id int primary key check (id = 1),
  username text not null,
  display_name text,
  given_name text,
  email text,
  user_handle bytea not null,
  created_at timestamptz not null
);

create table passkeys (
  id bytea primary key,
  name text not null,
  public_key bytea not null,
  sign_count bigint not null,
  aaguid bytea,
  transports text[],
  created_at timestamptz not null,
  last_used_at timestamptz
);

create table recovery_codes (
  id serial primary key,
  hash text not null,
  used_at timestamptz
);

create table emergency (
  id int primary key check (id = 1),
  enabled bool not null,
  password_hash text
);

create table apps (
  id uuid primary key,
  name text not null,
  url text not null,
  color text not null,
  icon text,
  protocol text not null default 'link',
  config jsonb not null default '{}',
  sort int not null,
  created_at timestamptz not null
);

create table audit_log (
  id bigserial primary key,
  at timestamptz not null,
  actor text not null,
  action text not null,
  ip inet,
  detail jsonb not null default '{}'
);

create table setup_state (
  id int primary key check (id = 1),
  completed_at timestamptz
);
