-- name: AddSection :one
-- Q8 (A5) with RETURNING: the section is added only to an event the signed-in
-- user owns; the stored row's id is returned. For any other event the SELECT
-- finds no row, so sqlc answers sql.ErrNoRows (S10).
INSERT INTO sections (event_id, name, capacity)
SELECT events.id, sqlc.arg(name), sqlc.arg(capacity)
FROM events
WHERE events.id = sqlc.arg(event_id) AND events.organizer_id = sqlc.arg(organizer_id)
RETURNING id;
