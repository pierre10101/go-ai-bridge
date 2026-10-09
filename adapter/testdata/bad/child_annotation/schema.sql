-- owner: organizer_id
CREATE TABLE IF NOT EXISTS events (
    id           INTEGER PRIMARY KEY,
    organizer_id INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS venues (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

-- owner: organizer_id
CREATE TABLE IF NOT EXISTS tours (
    code         TEXT,
    leg          INTEGER,
    organizer_id INTEGER NOT NULL,
    PRIMARY KEY (code, leg)
);

-- An unknown parent column.
-- owner: evnt_id -> events.organizer_id
CREATE TABLE IF NOT EXISTS sections (
    id       INTEGER PRIMARY KEY,
    event_id INTEGER NOT NULL
);

-- An unknown parent table.
-- owner: event_id -> evnts.organizer_id
CREATE TABLE IF NOT EXISTS rows_a (
    id       INTEGER PRIMARY KEY,
    event_id INTEGER NOT NULL
);

-- A parent that declares no owner.
-- owner: venue_id -> venues.owner_id
CREATE TABLE IF NOT EXISTS rows_b (
    id       INTEGER PRIMARY KEY,
    venue_id INTEGER NOT NULL
);

-- A parent column other than the one the parent's annotation names.
-- owner: event_id -> events.id
CREATE TABLE IF NOT EXISTS rows_c (
    id       INTEGER PRIMARY KEY,
    event_id INTEGER NOT NULL
);

-- A parent column whose type is not the parent key's (events.id INTEGER).
-- owner: event_id -> events.organizer_id
CREATE TABLE IF NOT EXISTS rows_d (
    id       INTEGER PRIMARY KEY,
    event_id TEXT NOT NULL
);

-- A parent without a single-column PRIMARY KEY.
-- owner: tour_code -> tours.organizer_id
CREATE TABLE IF NOT EXISTS rows_e (
    id        INTEGER PRIMARY KEY,
    tour_code TEXT NOT NULL
);

-- A chain that comes back to itself.
-- owner: b_id -> loop_b.a_id
CREATE TABLE IF NOT EXISTS loop_a (
    id   INTEGER PRIMARY KEY,
    b_id INTEGER NOT NULL
);

-- owner: a_id -> loop_a.b_id
CREATE TABLE IF NOT EXISTS loop_b (
    id   INTEGER PRIMARY KEY,
    a_id INTEGER NOT NULL
);

-- Malformed: no column after the table.
-- owner: event_id -> events
CREATE TABLE IF NOT EXISTS rows_f (
    id       INTEGER PRIMARY KEY,
    event_id INTEGER NOT NULL
);
