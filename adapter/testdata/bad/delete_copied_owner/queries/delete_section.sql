-- name: DeleteSection :execrows
-- Refused (A5): the WHERE compares the section's own copy of the owner.
DELETE FROM sections WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);
