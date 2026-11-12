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

	"golang.org/x/crypto/bcrypt"
)

// Input errors. Callers can test them with errors.Is.
var (
	ErrEmptyEmail      = errors.New("email must not be empty")
	ErrEmptyPassword   = errors.New("password must not be empty")
	ErrPasswordTooLong = errors.New("password must be at most 72 bytes")
)

// maxPasswordBytes is the most bytes bcrypt uses. Longer passwords are rejected rather than truncated.
const maxPasswordBytes = 72

// NormaliseEmail trims the email and lower-cases it. The result is the login identifier.
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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
// when the email is unknown, so a failed login takes as long as a wrong password.
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

// Authenticate reports whether email and password match the administrator account.
// The email is normalised first. A wrong email still runs a bcrypt comparison,
// so the response time does not reveal which emails exist. With no account,
// it returns false and no error.
func Authenticate(ctx context.Context, db *sql.DB, email, password string) (bool, error) {
	email = NormaliseEmail(email)

	var storedEmail, storedHash string
	err := db.QueryRowContext(ctx, `SELECT email, password_hash FROM admins WHERE id = 1`).
		Scan(&storedEmail, &storedHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("read admin: %w", err)
	}
	known := err == nil && email == storedEmail

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
// The email is normalised and the password is hashed. In one transaction it
// also deletes every session, so existing logins end.
func UpsertAdmin(ctx context.Context, db *sql.DB, email, password string) error {
	email = NormaliseEmail(email)
	if email == "" {
		return ErrEmptyEmail
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
		INSERT INTO admins (id, email, password_hash) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET email = excluded.email, password_hash = excluded.password_hash`,
		email, hash); err != nil {
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
