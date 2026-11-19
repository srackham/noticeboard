-- 0001_init: the schema and seed data (DESIGN.md sections 3.1-3.5).
--
-- This is the only migration. Earlier drafts were discarded before any
-- deployment. Later schema changes go in new numbered files, which are
-- forward-only.

CREATE TABLE timezones (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    label   TEXT    NOT NULL UNIQUE
                    CHECK (label = trim(label) AND length(label) BETWEEN 1 AND 60),
    zone    TEXT    NOT NULL UNIQUE CHECK (zone <> ''),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    sort    INTEGER NOT NULL UNIQUE
);

CREATE TABLE settings (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    title             TEXT    NOT NULL DEFAULT 'World Clocks'
                              CHECK (title = trim(title) AND length(title) <= 80),
    refresh_seconds   INTEGER NOT NULL DEFAULT 5
                              CHECK (refresh_seconds BETWEEN 1 AND 60),
    colour_background TEXT    NOT NULL DEFAULT '#d9d9d9'
                              CHECK (colour_background GLOB '#[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'),
    colour_label      TEXT    NOT NULL DEFAULT '#4169e1'
                              CHECK (colour_label GLOB '#[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'),
    colour_clock      TEXT    NOT NULL DEFAULT '#228b22'
                              CHECK (colour_clock GLOB '#[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'),
    colour_utc        TEXT    NOT NULL DEFAULT '#b22222'
                              CHECK (colour_utc GLOB '#[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'),
    table_page_size   INTEGER NOT NULL DEFAULT 20
                              CHECK (table_page_size BETWEEN 5 AND 100)
);

-- The administrator account. The username rule (printable, no white space) is
-- enforced in Go (internal/auth), because SQLite cannot express it.
CREATE TABLE admins (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    username      TEXT    NOT NULL CHECK (username <> ''),
    password_hash TEXT    NOT NULL CHECK (password_hash <> '')
);

CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    expires_at INTEGER NOT NULL
);

-- Seed: the settings singleton with its defaults.
INSERT INTO settings (id) VALUES (1);

-- Seed: the nine clocks from the mock-up (section 3.5), all enabled.
INSERT INTO timezones (label, zone, enabled, sort) VALUES
    ('UTC',              'UTC',               1, 1),
    ('New Zealand',      'Pacific/Auckland',  1, 2),
    ('India',            'Asia/Kolkata',      1, 3),
    ('Melbourne/Sydney', 'Australia/Sydney',  1, 4),
    ('China',            'Asia/Shanghai',     1, 5),
    ('Canada',           'America/Vancouver', 1, 6),
    ('Philippines',      'Asia/Manila',       1, 7),
    ('Russia',           'Europe/Moscow',     1, 8),
    ('Ukraine',          'Europe/Kyiv',       1, 9);
