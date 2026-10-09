-- name: RenameEvent :execrows
-- Refused (A4): events is owned by organizer_id, and nothing limits this
-- write to the signed-in user's events.
UPDATE events
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id);
