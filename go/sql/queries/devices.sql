-- name: CreateHostDevice :one
insert into devices (user_id, device_type, platform, label, hostname)
values ($1, 'host', $2, $3, $4)
returning *;

-- name: TouchDevice :exec
update devices
set last_seen_at = now(), app_version = $2
where id = $1;
