-- Reads after the claim (W1 allows them), in the same transaction: a
-- requested ticket this session still holds was not sold because its hold
-- has expired; the sold count is the postcondition.
-- name: CountStillHeld :one
SELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(session) AND id IN (sqlc.slice(ids));

-- name: CountSold :one
SELECT COUNT(*) FROM tickets WHERE sold_to = sqlc.arg(session) AND id IN (sqlc.slice(ids));
