-- ...then write: the seat may have been taken in between.
-- name: ClaimSeat :execrows
UPDATE seats SET held_by = sqlc.arg(held_by), held_at = sqlc.arg(now) WHERE id = sqlc.arg(id);
