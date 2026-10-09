-- name: RenameAnyEvent :execrows
-- Any event, whoever owns it: only admins call this, and cmd/server marks
-- "admin" with BypassOwnership (A4).
UPDATE events
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id);
