-- One statement frees the seat only if, at that moment, this session holds
-- it (Q6). The caller checks that exactly one row changed (S10).
-- name: ReleaseSeat :execrows
UPDATE seats
SET held_by = 0, held_at = 0
WHERE id = sqlc.arg(id) AND held_by = sqlc.arg(session);
