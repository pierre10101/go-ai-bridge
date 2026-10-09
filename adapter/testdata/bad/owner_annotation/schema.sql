-- Deliberately refused (A4): every owner annotation below but the one of
-- notes is wrong.

-- owner: organiser_id
CREATE TABLE events (
    id           INTEGER PRIMARY KEY,
    organizer_id INTEGER NOT NULL,
    title        TEXT    NOT NULL
);

-- owner: author
-- owner: author
CREATE TABLE notes (
    id     INTEGER PRIMARY KEY,
    author TEXT    NOT NULL,
    body   TEXT    NOT NULL
);

-- owner: score
CREATE TABLE ratings (
    id    INTEGER PRIMARY KEY,
    score REAL    NOT NULL
);

-- owner: id, author
CREATE TABLE drafts (
    id     INTEGER PRIMARY KEY,
    author TEXT    NOT NULL
);

-- owner: author

CREATE TABLE tags (
    id     INTEGER PRIMARY KEY,
    author TEXT    NOT NULL
);
