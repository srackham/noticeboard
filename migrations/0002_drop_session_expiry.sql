-- Sessions no longer expire on the server. A session ends on logout, when the
-- admin account is replaced, and when the server starts; the browser also signs
-- out after a period of inactivity.
ALTER TABLE sessions DROP COLUMN expires_at;
