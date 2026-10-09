-- name: RenameOwnEvent :execrows
-- Only the signed-in user's own event (A4: events is owned by organizer_id).
UPDATE events
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);
