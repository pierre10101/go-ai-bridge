-- name: InsertEvent :one
-- The organizer is the signed-in user (server:"user"), never a request field.
INSERT INTO events (organizer_id, created_as, title, starts_at)
VALUES (sqlc.arg(organizer_id), sqlc.arg(created_as), sqlc.arg(title), sqlc.arg(starts_at))
RETURNING id, organizer_id, created_as, title, starts_at;
