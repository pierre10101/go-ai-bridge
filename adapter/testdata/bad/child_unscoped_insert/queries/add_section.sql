-- name: AddSection :one
-- Refused (A5): the event id comes from the request and nothing proves the
-- event is the signed-in user's.
INSERT INTO sections (event_id, name, capacity)
VALUES (sqlc.arg(event_id), sqlc.arg(name), sqlc.arg(capacity))
RETURNING id;

-- name: RenameSection :execrows
-- Refused (A5): any section, by id, from an action organizers may call.
UPDATE sections SET name = sqlc.arg(name) WHERE id = sqlc.arg(id);
