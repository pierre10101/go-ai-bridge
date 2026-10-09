-- name: CountMySections :one
-- Q9 (A5): sections of the signed-in user's own events only.
SELECT COUNT(*) FROM sections
WHERE sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));
