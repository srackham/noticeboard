// Package auth manages the single administrator account (DESIGN.md section 4.1),
// password hashing, login checks and server-side sessions (section 4.2).
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Input errors. Callers can test them with errors.Is.
var (
	ErrInvalidUsername = errors.New("username must be one or more printable characters with no spaces")
	ErrEmptyPassword   = errors.New("password must not be empty")
	ErrPasswordTooLong = errors.New("password must be at most 72 bytes")
)

// maxPasswordBytes is the most bytes bcrypt uses. Longer passwords are rejected rather than truncated.
const maxPasswordBytes = 72

// NormaliseUsername trims surrounding white space and lower-cases the username.
// The result is the login identifier. Usernames are not case-sensitive.
func NormaliseUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// ValidateUsername reports whether a normalised username is valid. A valid
// username is one or more printable characters, none of them white space. The
// check is on runes, so it accepts non-ASCII letters but rejects control characters,
// spaces, tabs, line breaks and invalid UTF-8.
func ValidateUsername(username string) error {
	if username == "" || !utf8.ValidString(username) {
		return ErrInvalidUsername
	}
	for _, r := range username {
		if !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return ErrInvalidUsername
		}
	}
	return nil
}

// ValidatePassword reports whether pw may be used. It counts bytes, not characters.
func ValidatePassword(pw string) error {
	switch {
	case pw == "":
		return ErrEmptyPassword
	case len(pw) > maxPasswordBytes:
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns the bcrypt hash of pw at bcrypt.DefaultCost.
func HashPassword(pw string) (string, error) {
	if err := ValidatePassword(pw); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// checkPassword reports whether pw matches the bcrypt hash.
func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash is a bcrypt hash that no login can match. It is compared against
// when the username is unknown, so a failed login takes as long as a wrong password.
// It is generated on first use, at the same cost as real hashes.
var (
	dummyOnce sync.Once
	dummyHash []byte
	dummyErr  error
)

func getDummyHash() ([]byte, error) {
	dummyOnce.Do(func() {
		dummyHash, dummyErr = bcrypt.GenerateFromPassword([]byte("noticeboard-unused"), bcrypt.DefaultCost)
	})
	return dummyHash, dummyErr
}

// Authenticate reports whether username and password match the administrator account.
// The username is normalised first. A wrong username still runs a bcrypt comparison,
// so the response time does not reveal which usernames exist. With no account,
// it returns false and no error.
func Authenticate(ctx context.Context, db *sql.DB, username, password string) (bool, error) {
	username = NormaliseUsername(username)

	var storedUsername, storedHash string
	err := db.QueryRowContext(ctx, `SELECT username, password_hash FROM admins WHERE id = 1`).
		Scan(&storedUsername, &storedHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("read admin: %w", err)
	}
	known := err == nil && username == storedUsername

	hash := storedHash
	if !known {
		dummy, err := getDummyHash()
		if err != nil {
			return false, fmt.Errorf("prepare login check: %w", err)
		}
		hash = string(dummy)
	}
	match := checkPassword(hash, password)
	return known && match, nil
}

// AdminExists reports whether the administrator account has been created.
func AdminExists(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n); err != nil {
		return false, fmt.Errorf("count admins: %w", err)
	}
	return n > 0, nil
}

// UpsertAdmin creates the administrator account, or replaces it if one exists.
// The username is normalised and the password is hashed. In one transaction it
// also deletes every session, so existing logins end.
func UpsertAdmin(ctx context.Context, db *sql.DB, username, password string) error {
	username = NormaliseUsername(username)
	if err := ValidateUsername(username); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save admin: %w", err)
	}
	defer tx.Rollback() // no-op after a successful Commit

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO admins (id, username, password_hash) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET username = excluded.username, password_hash = excluded.password_hash`,
		username, hash); err != nil {
		return fmt.Errorf("save admin: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save admin: %w", err)
	}
	return nil
}
