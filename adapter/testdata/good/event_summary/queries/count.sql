-- name: CountSections :one
-- Every section of the event, whoever owns it: the summary is public.
SELECT COUNT(*) FROM sections WHERE event_id = sqlc.arg(event_id);

-- name: CountOwnEvent :one
-- 1 if the signed-in user organizes the event, 0 otherwise (also when
-- nobody is signed in: no event has organizer 0).
SELECT COUNT(*) FROM events WHERE id = sqlc.arg(id) AND organizer_id = sqlc.arg(organizer_id);
