-- name: DeleteOwnEvent :execrows
-- Q10 on an owned table (A4): only an event the signed-in user organizes.
-- Its sections go with it (ON DELETE CASCADE in schema.sql).
DELETE FROM events
WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);
