-- Signing in to Keystone is enough to open vault apps (the owner's choice):
-- installs still on the old 15-minute default stop asking again. Admin ›
-- Vault & secrets can set a window again. The change is logged.
with changed as (
  update settings
  set data = jsonb_set(data, '{vault,reconfirm_minutes}', '0')
  where (data -> 'vault' ->> 'reconfirm_minutes')::int = 15
  returning id
)
insert into audit_log (at, actor, action, detail)
select now(), 'system', 'settings.changed',
  '{"changes": [{"path": "vault.reconfirm_minutes", "old": 15, "new": 0}], "by": "update"}'::jsonb
from changed;
