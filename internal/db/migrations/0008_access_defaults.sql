-- Shared vault passwords stay the owner's until an admin chooses who may
-- use them: before accounts existed every app was effectively owner-only.
update apps set access = '{owner}' where protocol = 'vault' and access is null;

-- Usernames of deleted accounts are never handed out again: apps (SAML
-- NameID, LDAP uid, gate headers) know people by them.
create table retired_usernames (
  username text primary key,
  retired_at timestamptz not null default now()
);
