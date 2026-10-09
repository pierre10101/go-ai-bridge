-- name: CountSections :one
-- Refused (A5): a subquery on a table that does not inherit its owner (the
-- annotation of sections is refused).
SELECT COUNT(*) FROM sections
WHERE sections.event_id IN (SELECT events.id FROM events WHERE events.organizer_id = sqlc.arg(organizer_id));
