-- name: CreateUser :one
insert into users (email, display_name, password_hash, role, status)
values ($1, $2, $3, $4, $5)
returning *;

-- name: GetUserByEmail :one
select * from users where email = $1 limit 1;

-- name: GetUserByID :one
select * from users where id = $1 limit 1;

-- name: UpdateUserLastLogin :exec
update users
set last_login_at = now(), updated_at = now()
where id = $1;
