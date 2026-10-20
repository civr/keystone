-- WebAuthn authenticator flags (UP/UV/BE/BS byte) recorded at registration;
-- go-webauthn rejects logins whose backup-eligible flag differs from the
-- stored one, so synced passkeys (BE=1) need it persisted.
alter table passkeys add column flags smallint not null default 0;
