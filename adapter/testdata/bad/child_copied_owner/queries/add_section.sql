-- name: AddSection :one
-- Refused (A5): the signed-in user is copied onto the section, but the
-- event id comes from the request: B could add a section to A's event.
INSERT INTO sections (event_id, organizer_id, name)
VALUES (sqlc.arg(event_id), sqlc.arg(organizer_id), sqlc.arg(name))
RETURNING id;

-- name: RenameSection :execrows
-- Refused (A5): the WHERE compares the section's own copy of the owner.
UPDATE sections SET name = sqlc.arg(name)
WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);

-- name: CopySection :execrows
-- Refused (A5): a Q8 insert from the event row that also copies its owner.
INSERT INTO sections (event_id, organizer_id, name)
SELECT events.id, events.organizer_id, sqlc.arg(name)
FROM events
WHERE events.id = sqlc.arg(event_id) AND events.organizer_id = sqlc.arg(organizer_id);
