-- name: ListCustomerInvoices :many
SELECT seq, amount_cents, currency
FROM invoices
WHERE customer_id = sqlc.arg(customer_id) AND seq < sqlc.arg(after)
ORDER BY seq DESC
LIMIT sqlc.arg(limit);
