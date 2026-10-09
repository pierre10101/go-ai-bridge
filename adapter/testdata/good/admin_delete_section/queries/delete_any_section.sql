-- name: DeleteAnySection :execrows
-- Any section, whoever owns its event: only admins call this, and
-- cmd/server marks "admin" with BypassOwnership (A4, A5).
DELETE FROM sections
WHERE id = sqlc.arg(id);
