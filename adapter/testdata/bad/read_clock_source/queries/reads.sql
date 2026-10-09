-- name: CountHeldUntil :one
SELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(session) AND expires_at <= sqlc.arg(cutoff) AND id IN (sqlc.slice(ids));

-- name: FindLiveHold :one
SELECT id, expires_at FROM tickets WHERE held_by = sqlc.arg(session) AND expires_at > sqlc.arg(now) - 600;

-- A comparison with a literal, not the current time.
-- name: CountEndedBefore :one
SELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(session) AND expires_at <= 1800000000;
