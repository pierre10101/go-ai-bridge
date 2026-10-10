-- name: ListEventSections :many
-- Q5 with one INNER JOIN: page sections of an event, carrying the event title.
SELECT sections.id, sections.name, sections.capacity, events.title
FROM sections
JOIN events ON sections.event_id = events.id
WHERE sections.event_id = sqlc.arg(event_id) AND sections.id < sqlc.arg(after)
ORDER BY sections.id DESC
LIMIT sqlc.arg(limit);
