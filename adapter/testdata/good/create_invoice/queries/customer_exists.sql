-- name: CustomerExists :one
SELECT COUNT(*) FROM customers WHERE id = ?;
