-- Role-based access: roles are sets of permissions; they are given to
-- accounts or groups, globally or for one app (application admins). The
-- owner is always a super admin and needs no row here.
create table roles (
  id text primary key,
  name text not null unique,
  description text not null default '',
  permissions text[] not null,
  builtin boolean not null default false,
  created_at timestamptz not null default now()
);

insert into roles (id, name, description, permissions, builtin) values
  ('superadmin', 'Super Admin', 'Full control of Keystone.', '{*}', true),
  ('admin', 'Administrator', 'Manages users, apps and settings; cannot change roles.',
    '{users.read,users.write,groups.write,apps.read,apps.write,auth.write,idp.write,security.write,sessions.manage,audit.read,vault.write,branding.write,system.write}', true),
  ('app_admin', 'Application Admin', 'Manages the apps it is given.', '{apps.read,apps.write}', true),
  ('security_admin', 'Security Admin', 'Security settings, sessions and the audit log.',
    '{security.write,auth.write,sessions.manage,audit.read,users.read,apps.read}', true),
  ('auditor', 'Read-only Auditor', 'Sees configuration and the audit log; changes nothing.',
    '{users.read,apps.read,audit.read}', true);

create table groups (
  id uuid primary key default gen_random_uuid(),
  name text not null unique,
  description text not null default '',
  created_at timestamptz not null default now()
);

-- account_id: 'owner' or a users.id; members of deleted accounts are removed
-- with them (accounts.Delete).
create table group_members (
  group_id uuid not null references groups (id) on delete cascade,
  account_id text not null,
  primary key (group_id, account_id)
);
create index group_members_account on group_members (account_id);

create table role_assignments (
  id uuid primary key default gen_random_uuid(),
  role_id text not null references roles (id) on delete cascade,
  account_id text,
  group_id uuid references groups (id) on delete cascade,
  app_id uuid references apps (id) on delete cascade,
  check ((account_id is null) <> (group_id is null))
);
create index role_assignments_account on role_assignments (account_id);
create index role_assignments_group on role_assignments (group_id);

-- Admin users become Administrators.
insert into role_assignments (role_id, account_id)
  select 'admin', id::text from users where role = 'admin';
