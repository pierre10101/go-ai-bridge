-- More shapes the grammar does not recognise. Each is refused, none is guessed.

-- name: ListInvoices :many
SELECT id, seq FROM invoices WHERE customer_id = ?;

-- name: InvoiceWithCustomer :one
SELECT seq FROM invoices JOIN customers ON customers.id = invoices.customer_id WHERE seq = ?;

-- name: UpsertCustomer :one
INSERT INTO customers (id, name) VALUES (?, ?) ON CONFLICT (id) DO UPDATE SET name = excluded.name RETURNING id;

-- name: RenameCustomer :one
UPDATE customers SET name = ? WHERE id = ? RETURNING id;

-- name: CustomerByIDOrName :one
SELECT id FROM customers WHERE id = ? OR name = ?;
