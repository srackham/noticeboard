package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"noticeboard/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

func TestNormaliseEmail(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Admin@Example.COM", "admin@example.com"},
		{"  admin@example.com \t", "admin@example.com"},
		{"admin@example.com", "admin@example.com"},
		{"   ", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormaliseEmail(tt.in); got != tt.want {
			t.Errorf("NormaliseEmail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name string
		pw   string
		want error
	}{
		{name: "empty", pw: "", want: ErrEmptyPassword},
		{name: "one character", pw: "x"},
		{name: "72 bytes", pw: strings.Repeat("a", 72)},
		{name: "73 bytes", pw: strings.Repeat("a", 73), want: ErrPasswordTooLong},
		{name: "72 bytes of two-byte characters", pw: strings.Repeat("é", 36)},
		{name: "74 bytes of two-byte characters", pw: strings.Repeat("é", 37), want: ErrPasswordTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.pw)
			if !errors.Is(err, tt.want) && !(err == nil && tt.want == nil) {
				t.Errorf("ValidatePassword = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Error("hash contains the plaintext password")
	}
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != bcrypt.DefaultCost {
		t.Errorf("cost = %d (%v), want %d", cost, err, bcrypt.DefaultCost)
	}
	if !checkPassword(hash, "correct horse") {
		t.Error("checkPassword rejected the right password")
	}
	if checkPassword(hash, "wrong") {
		t.Error("checkPassword accepted a wrong password")
	}
	if _, err := HashPassword(""); !errors.Is(err, ErrEmptyPassword) {
		t.Errorf("HashPassword(\"\") error = %v, want ErrEmptyPassword", err)
	}
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	if err := UpsertAdmin(ctx, database, "Admin@Example.com", "correct horse"); err != nil {
		t.Fatalf("UpsertAdmin: %v", err)
	}

	tests := []struct {
		name, email, password string
		want                  bool
	}{
		{name: "right email and password", email: "admin@example.com", password: "correct horse", want: true},
		{name: "email is normalised", email: "  ADMIN@example.com ", password: "correct horse", want: true},
		{name: "wrong password", email: "admin@example.com", password: "wrong"},
		{name: "unknown email", email: "other@example.com", password: "correct horse"},
		{name: "empty input", email: "", password: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Authenticate(ctx, database, tt.email, tt.password)
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			if got != tt.want {
				t.Errorf("Authenticate = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthenticateWithNoAdmin(t *testing.T) {
	database := openTestDB(t)
	got, err := Authenticate(context.Background(), database, "admin@example.com", "anything")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got {
		t.Error("Authenticate succeeded with no admin account")
	}
}

func TestAdminExists(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)

	if ok, err := AdminExists(ctx, database); err != nil || ok {
		t.Fatalf("AdminExists before upsert = %v, %v; want false, nil", ok, err)
	}
	if err := UpsertAdmin(ctx, database, "admin@example.com", "pw"); err != nil {
		t.Fatalf("UpsertAdmin: %v", err)
	}
	if ok, err := AdminExists(ctx, database); err != nil || !ok {
		t.Errorf("AdminExists after upsert = %v, %v; want true, nil", ok, err)
	}
}

func TestUpsertAdminReplacesAccount(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)

	if err := UpsertAdmin(ctx, database, "first@example.com", "first password"); err != nil {
		t.Fatalf("first UpsertAdmin: %v", err)
	}
	if err := UpsertAdmin(ctx, database, "second@example.com", "second password"); err != nil {
		t.Fatalf("second UpsertAdmin: %v", err)
	}

	if ok, _ := Authenticate(ctx, database, "first@example.com", "first password"); ok {
		t.Error("old account still logs in")
	}
	if ok, _ := Authenticate(ctx, database, "second@example.com", "second password"); !ok {
		t.Error("new account cannot log in")
	}

	var rows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("admin rows = %d (%v), want 1", rows, err)
	}
}

func TestUpsertAdminEndsExistingSessions(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	now := time.Date(2024, time.June, 1, 12, 0, 0, 0, time.UTC)

	if err := UpsertAdmin(ctx, database, "admin@example.com", "pw"); err != nil {
		t.Fatalf("UpsertAdmin: %v", err)
	}
	token, err := CreateSession(ctx, database, now)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := UpsertAdmin(ctx, database, "admin@example.com", "new pw"); err != nil {
		t.Fatalf("second UpsertAdmin: %v", err)
	}
	if ok, _ := ValidSession(ctx, database, token, now); ok {
		t.Error("session survived replacing the admin account")
	}
}

func TestUpsertAdminRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name, email, password string
		want                  error
	}{
		{name: "empty email", email: "  ", password: "pw", want: ErrEmptyEmail},
		{name: "empty password", email: "admin@example.com", password: "", want: ErrEmptyPassword},
		{name: "password too long", email: "admin@example.com", password: strings.Repeat("a", 73), want: ErrPasswordTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := openTestDB(t)
			err := UpsertAdmin(ctx, database, tt.email, tt.password)
			if !errors.Is(err, tt.want) {
				t.Fatalf("UpsertAdmin error = %v, want %v", err, tt.want)
			}
			if ok, _ := AdminExists(ctx, database); ok {
				t.Error("rejected input still created an account")
			}
		})
	}
}

func TestUpsertAdminStoresHashNotPassword(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	if err := UpsertAdmin(ctx, database, "admin@example.com", "plain secret"); err != nil {
		t.Fatalf("UpsertAdmin: %v", err)
	}

	var email, hash string
	if err := database.QueryRow(`SELECT email, password_hash FROM admins WHERE id = 1`).Scan(&email, &hash); err != nil {
		t.Fatalf("read admin: %v", err)
	}
	if email != "admin@example.com" {
		t.Errorf("stored email = %q, want normalised", email)
	}
	if hash == "plain secret" || !strings.HasPrefix(hash, "$2") {
		t.Errorf("stored password_hash = %q, want a bcrypt hash", hash)
	}
}
