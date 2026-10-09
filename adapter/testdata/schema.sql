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

-- held_by: the session that holds the seat (0 when nobody does), until
-- expires_at (unix seconds). The holder is always the server-set session,
-- never an id the caller sends.
CREATE TABLE IF NOT EXISTS seats (
    id         INTEGER PRIMARY KEY,
    held_by    INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0
);

-- One row per ticket of an event (confirm_many). held_by: the session that
-- holds the ticket ('' when nobody does), until expires_at (unix seconds).
-- sold_to: the session it was sold to ('' while it is not sold).
CREATE TABLE IF NOT EXISTS tickets (
    id         INTEGER PRIMARY KEY,
    held_by    TEXT    NOT NULL DEFAULT '',
    expires_at INTEGER NOT NULL DEFAULT 0,
    sold_to    TEXT    NOT NULL DEFAULT ''
);
