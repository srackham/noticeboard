-- 0002_admin_username: the administrator logs in with a username rather than an
-- email address (DESIGN.md section 4.1). The column is renamed, not replaced, so
-- an existing account and its password keep working. The rule that a username is
-- printable and has no white space is enforced in Go (internal/auth).

ALTER TABLE admins RENAME COLUMN email TO username;
