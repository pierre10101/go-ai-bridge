-- name: AddSection :execrows
INSERT INTO sections (event_id, name, capacity)
SELECT events.id, sqlc.arg(name), sqlc.arg(capacity)
FROM events
WHERE events.id = sqlc.arg(event_id) AND events.organizer_id = sqlc.arg(organizer_id);
