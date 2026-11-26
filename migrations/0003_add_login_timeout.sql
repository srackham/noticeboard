-- The admin pages sign out after a period without user activity. The period is
-- a setting, in minutes, so it can be fractional (for example 1.5).
ALTER TABLE settings ADD COLUMN login_timeout REAL NOT NULL DEFAULT 15
    CHECK (login_timeout BETWEEN 0.5 AND 1440);
