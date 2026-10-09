-- name: DeleteOwnSection :execrows
-- Refused (A5) in a Public action, although it has the Q9 proof.
DELETE FROM sections
WHERE sections.id = sqlc.arg(id)
  AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));

-- name: DeleteOwnEvent :execrows
-- Refused (A4) in a Public action, although it has the owner condition.
DELETE FROM events WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);
