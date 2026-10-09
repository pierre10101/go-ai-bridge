-- One statement claims the seat only if, at that moment, it is free or its
-- hold was taken 10 minutes (600 seconds) or more before now (Q6). The caller
-- checks that exactly one row changed (S10).
-- name: ClaimSeat :execrows
UPDATE seats
SET held_by = sqlc.arg(held_by), held_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND (held_by = 0 OR held_at <= sqlc.arg(now) - 600);
