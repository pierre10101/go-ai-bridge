-- name: CountMyNotes :one
SELECT COUNT(*) FROM notes WHERE author = sqlc.arg(author);
