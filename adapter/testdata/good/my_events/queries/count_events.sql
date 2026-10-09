-- name: CountMyEvents :one
SELECT COUNT(*) FROM events WHERE organizer_id = sqlc.arg(organizer_id);
