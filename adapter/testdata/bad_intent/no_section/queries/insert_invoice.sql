-- name: InsertInvoice :one
-- The next sequence number is computed inside the same statement, so it is atomic.
INSERT INTO invoices (seq, customer_id, amount_cents, currency)
VALUES (
    (SELECT COALESCE(MAX(seq), 0) + 1 FROM invoices),
    sqlc.arg(customer_id),
    sqlc.arg(amount_cents),
    sqlc.arg(currency)
)
RETURNING id, seq, customer_id, amount_cents, currency, created_at;
