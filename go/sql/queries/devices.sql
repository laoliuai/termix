-- name: CreateHostDevice :one
insert into devices (user_id, device_type, platform, label, hostname)
values ($1, 'host', $2, $3, $4)
returning *;

-- name: TouchDevice :exec
update devices
set last_seen_at = now(), app_version = $2
where id = $1;

-- name: GetDeviceForUser :one
select *
from devices
where id = $1
  and user_id = $2
  and disabled_at is null
limit 1;
