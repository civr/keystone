-- Huisstijl: the name, logo and colours every page shows (Instellingen ›
-- Huisstijl). One row; missing means Keystone's own.
create table branding (
  id int primary key check (id = 1),
  data jsonb not null,
  updated_at timestamptz not null
);
