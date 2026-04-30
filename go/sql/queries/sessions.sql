-- name: CreateSession :one
insert into sessions (
  user_id, host_device_id, name, tool, launch_command, cwd, cwd_label, tmux_session_name, status
)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
returning *;

-- name: UpdateSessionStatus :one
update sessions
set status = $2,
    last_error = $3,
    last_exit_code = $4,
    last_activity_at = now(),
    updated_at = now()
where id = $1
returning *;

-- name: GetSessionForUser :one
select sessions.*,
       devices.platform as host_platform,
       devices.label    as host_device_label
from sessions
join devices on devices.id = sessions.host_device_id
where sessions.id = $1 and sessions.user_id = $2
limit 1;

-- name: ListUserSessions :many
select sessions.*,
       devices.platform as host_platform,
       devices.label    as host_device_label
from sessions
join devices on devices.id = sessions.host_device_id
where sessions.user_id = $1
order by sessions.last_activity_at desc;

-- name: TouchSessionHeartbeat :one
update sessions
set status = $4,
    last_seen_at = now(),
    updated_at = now()
where id = $1
  and user_id = $2
  and host_device_id = $3
returning *;
