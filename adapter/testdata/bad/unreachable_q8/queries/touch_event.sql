-- name: TouchEvent :execrows
UPDATE events
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);
