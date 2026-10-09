-- name: InsertInvoice :one
-- An AI "fixed" duplicate inserts by ignoring them. The row may silently not be written.
INSERT OR IGNORE INTO invoices (seq, customer_id, amount_cents, currency)
VALUES (
    (SELECT COALESCE(MAX(seq), 0) + 1 FROM invoices),
    sqlc.arg(customer_id),
    sqlc.arg(amount_cents),
    sqlc.arg(currency)
)
RETURNING id, seq, customer_id, amount_cents, currency, created_at;
