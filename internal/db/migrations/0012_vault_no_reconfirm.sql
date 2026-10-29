-- Signing in to Keystone is enough to open vault apps: installs still on the
-- old 15-minute default stop asking again (Admin › Vault & secrets can set a
-- window).
update settings
set data = jsonb_set(data, '{vault,reconfirm_minutes}', '0')
where (data -> 'vault' ->> 'reconfirm_minutes')::int = 15;
