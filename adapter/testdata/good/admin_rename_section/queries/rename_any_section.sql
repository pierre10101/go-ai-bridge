-- name: RenameAnySection :execrows
-- Any section, whoever owns its event: only admins call this, and
-- cmd/server marks "admin" with BypassOwnership (A4, A5).
UPDATE sections
SET name = sqlc.arg(name)
WHERE id = sqlc.arg(id);
