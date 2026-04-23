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
