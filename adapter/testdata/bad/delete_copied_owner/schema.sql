-- owner: organizer_id
CREATE TABLE IF NOT EXISTS events (
    id           INTEGER PRIMARY KEY,
    organizer_id INTEGER NOT NULL
);

-- organizer_id here is a copy of the event's organizer: it proves nothing,
-- since it was written next to whatever event the row names.
-- owner: event_id -> events.organizer_id
CREATE TABLE IF NOT EXISTS sections (
    id           INTEGER PRIMARY KEY,
    event_id     INTEGER NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    organizer_id INTEGER NOT NULL,
    name         TEXT    NOT NULL
);
