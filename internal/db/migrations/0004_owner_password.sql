-- A Keystone account made in the short setup has its own password (argon2),
-- checked instead of AD until AD is set up and linked.
alter table owner add column password_hash text;
