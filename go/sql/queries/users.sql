-- name: GetUserByEmail :one
select * from users where email = $1 limit 1;

-- name: UpdateUserLastLogin :exec
update users
set last_login_at = now(), updated_at = now()
where id = $1;
