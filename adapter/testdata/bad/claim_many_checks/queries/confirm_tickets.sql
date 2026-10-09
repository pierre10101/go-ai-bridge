-- One statement sells every requested ticket to this session, only if, at
-- that moment, this session holds it and its hold has not expired (Q6). The
-- IN list comes last (Q7) and is on the table's key, so the caller can check
-- that one row changed per requested ticket (S11).
-- name: ConfirmTickets :execrows
UPDATE tickets
SET sold_to = sqlc.arg(session), held_by = ''
WHERE held_by = sqlc.arg(session) AND expires_at > sqlc.arg(now) AND id IN (sqlc.slice(ids));
