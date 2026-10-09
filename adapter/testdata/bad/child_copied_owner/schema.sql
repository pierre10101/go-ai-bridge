-- owner: organizer_id
CREATE TABLE IF NOT EXISTS events (
    id           INTEGER PRIMARY KEY,
    organizer_id INTEGER NOT NULL
);

-- organizer_id here is a copy of the event's organizer: it proves nothing,
-- since a request could name any event next to the caller's own id.
-- owner: event_id -> events.organizer_id
CREATE TABLE IF NOT EXISTS sections (
    id           INTEGER PRIMARY KEY,
    event_id     INTEGER NOT NULL REFERENCES events (id),
    organizer_id INTEGER NOT NULL,
    name         TEXT    NOT NULL
);
