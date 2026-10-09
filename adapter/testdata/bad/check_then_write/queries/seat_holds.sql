-- The "check": is somebody already holding the seat? (read first...)
-- name: SeatHolds :one
SELECT COUNT(*) FROM seats WHERE id = ? AND held_by = ?;
