-- name: RenameEvent :execrows
-- Refused (A4): scoped by organizer_id, but the action binds it to a value
-- the caller sends, so anyone can name any organizer.
UPDATE events
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);

-- name: CopyEvent :one
-- Refused (A4): the new event's owner is a value the caller sends.
INSERT INTO events (organizer_id, created_as, title, starts_at)
VALUES (sqlc.arg(organizer_id), 'organizer', sqlc.arg(title), sqlc.arg(starts_at))
RETURNING id;
