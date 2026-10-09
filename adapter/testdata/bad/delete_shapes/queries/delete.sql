-- name: DeleteEverySection :execrows
-- Refused (Q10): no WHERE removes every row.
DELETE FROM sections;

-- name: DeleteByName :execrows
-- Refused (Q10): name is not the key; it could match any number of rows.
DELETE FROM sections WHERE name = sqlc.arg(name);

-- name: DeleteSmallSections :execrows
-- Refused (Q10): a range is not a key.
DELETE FROM sections WHERE id = sqlc.arg(id) AND capacity < sqlc.arg(capacity);

-- name: DeleteSectionOrName :execrows
-- Refused (Q10): an OR widens the delete beyond the key.
DELETE FROM sections WHERE id = sqlc.arg(id) OR name = sqlc.arg(name);

-- name: DeleteSectionReturning :execrows
-- Refused (Q10): a delete answers only with the number of rows it removed.
DELETE FROM sections WHERE id = sqlc.arg(id) RETURNING name;

-- name: DeleteCustomer :execrows
-- Refused (Q10): invoices.customer_id references customers without
-- ON DELETE CASCADE or RESTRICT, so schema.sql does not say what happens
-- to a deleted customer's invoices.
DELETE FROM customers WHERE id = sqlc.arg(id);
