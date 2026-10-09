-- name: DeleteEvent :execrows
-- Refused (A4): any event, by id, from an action organizers may call.
DELETE FROM events WHERE id = sqlc.arg(id);
