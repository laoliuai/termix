-- name: UpsertControlLease :one
insert into control_leases (
  session_id,
  controller_device_id,
  lease_version,
  granted_at,
  expires_at
)
values (
  sqlc.arg(session_id),
  sqlc.arg(controller_device_id),
  1,
  sqlc.arg(now),
  sqlc.arg(expires_at)
)
on conflict (session_id) do update
set controller_device_id = excluded.controller_device_id,
    lease_version = control_leases.lease_version + 1,
    granted_at = sqlc.arg(now),
    expires_at = excluded.expires_at
where control_leases.expires_at <= sqlc.arg(now)
   or control_leases.controller_device_id = excluded.controller_device_id
returning *;

-- name: GetActiveControlLease :one
select *
from control_leases
where session_id = sqlc.arg(session_id)
  and expires_at > sqlc.arg(now)
limit 1;

-- name: RenewControlLease :one
update control_leases
set lease_version = lease_version + 1,
    expires_at = sqlc.arg(expires_at)
where session_id = sqlc.arg(session_id)
  and controller_device_id = sqlc.arg(controller_device_id)
  and lease_version = sqlc.arg(lease_version)
  and expires_at > sqlc.arg(now)
returning *;

-- name: ReleaseControlLease :one
delete from control_leases
where session_id = sqlc.arg(session_id)
  and controller_device_id = sqlc.arg(controller_device_id)
  and lease_version = sqlc.arg(lease_version)
returning *;
