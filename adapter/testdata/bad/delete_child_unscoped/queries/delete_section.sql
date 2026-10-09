-- name: DeleteSection :execrows
-- Refused (A5): any section, by id, from an action organizers may call.
DELETE FROM sections WHERE id = sqlc.arg(id);
