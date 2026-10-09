-- The fixture app's schema. One file, plain SQL: sqlc reads it, store.Open applies it.

CREATE TABLE IF NOT EXISTS customers (
    id   INTEGER PRIMARY KEY,
    name TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS invoices (
    id           INTEGER PRIMARY KEY,
    seq          INTEGER NOT NULL UNIQUE,
    customer_id  INTEGER NOT NULL REFERENCES customers (id),
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0),
    currency     TEXT    NOT NULL,
    created_at   TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- held_by = 0: free. held_at: when the hold was taken (unix seconds).
CREATE TABLE IF NOT EXISTS seats (
    id      INTEGER PRIMARY KEY,
    held_by INTEGER NOT NULL DEFAULT 0,
    held_at INTEGER NOT NULL DEFAULT 0
);
