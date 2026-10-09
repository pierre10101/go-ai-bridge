-- Reads after the claim (W1 allows them): they explain a release that
-- changed nothing, and show the seat afterwards.
-- name: CountSeats :one
SELECT COUNT(*) FROM seats WHERE id = ?;

-- name: SeatHolder :one
SELECT held_by, held_at FROM seats WHERE id = ?;
