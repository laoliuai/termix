-- name: InsertRefreshToken :one
insert into refresh_tokens (user_id, device_id, token_hash, expires_at)
values ($1, $2, $3, $4)
returning *;

-- name: GetActiveRefreshTokenByHash :one
select *
from refresh_tokens
where token_hash = $1
  and revoked_at is null
  and expires_at > now()
limit 1;

-- name: RevokeRefreshToken :exec
update refresh_tokens
set revoked_at = now()
where token_hash = $1
  and revoked_at is null;
