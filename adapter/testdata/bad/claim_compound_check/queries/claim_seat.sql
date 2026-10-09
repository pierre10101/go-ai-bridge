-- One statement claims the seat only if, at that moment, it is free or its
-- hold has expired (expires_at is now or earlier) (Q6). The holder is the
-- server-set session; the new hold expires 10 minutes (600 seconds) after
-- now. The caller checks that exactly one row changed (S10).
-- name: ClaimSeat :execrows
UPDATE seats
SET held_by = sqlc.arg(session), expires_at = sqlc.arg(now) + 600
WHERE id = sqlc.arg(id) AND (held_by = 0 OR expires_at <= sqlc.arg(now));
