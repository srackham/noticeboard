package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// SessionLifetime is how long a login lasts. Sessions do not renew.
const SessionLifetime = 12 * time.Hour

// newSessionToken returns 32 random bytes encoded as base64url. Only the hash of this token is stored.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// TokenHash returns the hex SHA-256 of a session token. This is the value stored in sessions.token_hash.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession starts a session at now and returns its token. The token is
// the caller's to keep (in the cookie). The database keeps only its hash.
func CreateSession(ctx context.Context, db *sql.DB, now time.Time) (string, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, expires_at) VALUES (?, ?)`,
		TokenHash(token), now.Add(SessionLifetime).Unix()); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// ValidSession reports whether token names a session that has not expired at now.
// An empty token is never valid.
func ValidSession(ctx context.Context, db *sql.DB, token string, now time.Time) (bool, error) {
	if token == "" {
		return false, nil
	}
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		TokenHash(token), now.Unix()).Scan(&n); err != nil {
		return false, fmt.Errorf("check session: %w", err)
	}
	return n > 0, nil
}

// DeleteSession ends the session for token. An empty token is ignored.
func DeleteSession(ctx context.Context, db *sql.DB, token string) error {
	if token == "" {
		return nil
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, TokenHash(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteAllSessions ends every session. The server calls it when it starts, so a
// login from a previous run does not survive a restart.
func DeleteAllSessions(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes sessions that have expired at now.
func DeleteExpiredSessions(ctx context.Context, db *sql.DB, now time.Time) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.Unix()); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
