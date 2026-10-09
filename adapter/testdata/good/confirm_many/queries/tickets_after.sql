-- Reads after the claim (W1 allows them), in the same transaction: a
-- requested ticket this session still holds whose hold ended no later than
-- now was not sold because its hold has expired (Q1 compares with the
-- server-set now, before the IN list: Q7); the sold count is the
-- postcondition.
-- name: CountStillHeld :one
SELECT COUNT(*) FROM tickets WHERE held_by = sqlc.arg(session) AND expires_at <= sqlc.arg(now) AND id IN (sqlc.slice(ids));

-- name: CountSold :one
SELECT COUNT(*) FROM tickets WHERE sold_to = sqlc.arg(session) AND id IN (sqlc.slice(ids));
