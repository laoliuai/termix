create table control_leases (
  session_id uuid primary key references sessions(id) on delete cascade,
  controller_device_id uuid not null references devices(id),
  lease_version bigint not null,
  granted_at timestamptz not null default now(),
  expires_at timestamptz not null
);

create index control_leases_expires_at_idx on control_leases(expires_at);
