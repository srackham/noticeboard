-- The login timeout may now be as short as 0.1 minutes. SQLite cannot change a
-- CHECK constraint in place, so the singleton table is rebuilt with the new
-- constraint and the row is copied across.
CREATE TABLE settings_new (
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
                              CHECK (table_page_size BETWEEN 5 AND 100),
    login_timeout     REAL    NOT NULL DEFAULT 15
                              CHECK (login_timeout BETWEEN 0.1 AND 1440)
);

INSERT INTO settings_new (id, title, refresh_seconds, colour_background, colour_label,
                          colour_clock, colour_utc, table_page_size, login_timeout)
SELECT id, title, refresh_seconds, colour_background, colour_label,
       colour_clock, colour_utc, table_page_size, login_timeout
FROM settings;

DROP TABLE settings;
ALTER TABLE settings_new RENAME TO settings;
