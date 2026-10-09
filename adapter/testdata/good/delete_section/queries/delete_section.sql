-- name: DeleteOwnSection :execrows
-- Q10 with the Q9 proof (A5): only a section of an event the signed-in
-- user owns. For any other section nothing is deleted (S10).
DELETE FROM sections
WHERE sections.id = sqlc.arg(id)
  AND sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));
