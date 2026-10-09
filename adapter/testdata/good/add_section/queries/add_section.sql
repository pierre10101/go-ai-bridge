-- name: AddSection :execrows
-- Q8 (A5): the section is added only to an event the signed-in user owns.
-- For any other event the SELECT finds no row, so nothing is added (S10).
INSERT INTO sections (event_id, name, capacity)
SELECT events.id, sqlc.arg(name), sqlc.arg(capacity)
FROM events
WHERE events.id = sqlc.arg(event_id) AND events.organizer_id = sqlc.arg(organizer_id);
