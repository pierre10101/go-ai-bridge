-- A parameter after the slice: sqlc numbers it as if the slice were one
-- value, so SQLite would bind it to one of the list's entries.
-- name: ConfirmSliceFirst :execrows
UPDATE tickets
SET sold_to = sqlc.arg(session), held_by = ''
WHERE id IN (sqlc.slice(ids)) AND held_by = sqlc.arg(session) AND expires_at > sqlc.arg(now);

-- IN on a column that is not the key: one entry may match many rows.
-- name: ConfirmByHolder :execrows
UPDATE tickets
SET sold_to = sqlc.arg(session)
WHERE expires_at > sqlc.arg(now) AND held_by IN (sqlc.slice(holders));

-- name: ConfirmInGroup :execrows
UPDATE tickets
SET sold_to = sqlc.arg(session)
WHERE held_by = sqlc.arg(session) AND (expires_at = 0 OR id IN (sqlc.slice(ids)));

-- name: CountListed :one
SELECT COUNT(*) FROM tickets WHERE id IN (1, 2);

-- name: CountTickets :one
SELECT COUNT(*) FROM tickets WHERE id IN (sqlc.slice(ids));
