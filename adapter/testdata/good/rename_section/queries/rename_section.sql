-- name: RenameOwnSection :execrows
-- Q9 (A5): only a section of an event the signed-in user owns.
UPDATE sections
SET name = sqlc.arg(name)
WHERE sections.id = sqlc.arg(id)
  AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));
