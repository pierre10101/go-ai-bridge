-- The current time after the IN list: SQLite would bind it to a list entry.
-- name: CountAfterList :one
SELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(session) AND id IN (sqlc.slice(ids)) AND expires_at <= sqlc.arg(now);

-- The current time on the left.
-- name: CountClockLeft :one
SELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(session) AND sqlc.arg(now) >= expires_at;

-- A comparison with the current time in a keyset page.
-- name: PageEnded :many
SELECT id, expires_at FROM tickets WHERE held_by = sqlc.arg(session) AND expires_at <= sqlc.arg(now) AND id < sqlc.arg(after) ORDER BY id DESC LIMIT 20;
