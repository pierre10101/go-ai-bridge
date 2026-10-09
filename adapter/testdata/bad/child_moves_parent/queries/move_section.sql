-- name: MoveSection :execrows
-- Refused (A5): the section is proved to be the signed-in user's, but the
-- claim moves it to another event, which nothing proves is theirs.
UPDATE sections
SET event_id = sqlc.arg(new_event_id), name = sqlc.arg(name)
WHERE sections.id = sqlc.arg(id)
  AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));
