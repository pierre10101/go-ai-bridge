-- ...then write: the seat may have been taken in between.
-- name: ClaimSeat :execrows
UPDATE seats SET held_by = sqlc.arg(session), expires_at = sqlc.arg(now) + 600 WHERE id = sqlc.arg(id);
