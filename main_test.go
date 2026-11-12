package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"noticeboard/internal/auth"
	"noticeboard/internal/db"
)

// fakePrompt answers each prompt with the next value in answers.
func fakePrompt(answers ...string) promptFunc {
	return func(string) (string, error) {
		if len(answers) == 0 {
			return "", errors.New("unexpected prompt")
		}
		answer := answers[0]
		answers = answers[1:]
		return answer, nil
	}
}

// openDB opens the database at path, which the test has created.
func openDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

func TestAdminInitCreatesAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noticeboard.db")
	if err := adminInit(path, "Admin@Example.com", fakePrompt("pw one", "pw one")); err != nil {
		t.Fatalf("adminInit: %v", err)
	}

	sqlDB := openDB(t, path)
	ok, err := auth.Authenticate(context.Background(), sqlDB, "admin@example.com", "pw one")
	if err != nil || !ok {
		t.Errorf("Authenticate = %v, %v; want true, nil", ok, err)
	}
}

func TestAdminInitMismatchChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noticeboard.db")
	err := adminInit(path, "admin@example.com", fakePrompt("first", "second"))
	if err == nil {
		t.Fatal("adminInit succeeded with mismatched passwords")
	}

	sqlDB := openDB(t, path)
	if ok, _ := auth.AdminExists(context.Background(), sqlDB); ok {
		t.Error("mismatched passwords still created an account")
	}
}

func TestAdminInitRejectsBadInput(t *testing.T) {
	tests := []struct {
		name, email string
		answers     []string
		want        error
	}{
		{name: "empty email", email: "   ", answers: nil, want: auth.ErrEmptyEmail},
		{name: "empty password", email: "admin@example.com", answers: []string{"", ""}, want: auth.ErrEmptyPassword},
		{name: "password too long", email: "admin@example.com", answers: []string{string(make([]byte, 73))}, want: auth.ErrPasswordTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "noticeboard.db")
			err := adminInit(path, tt.email, fakePrompt(tt.answers...))
			if !errors.Is(err, tt.want) {
				t.Errorf("adminInit error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAdminInitReplacesAccountAndEndsSessions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noticeboard.db")
	if err := adminInit(path, "admin@example.com", fakePrompt("old pw", "old pw")); err != nil {
		t.Fatalf("first adminInit: %v", err)
	}

	sqlDB := openDB(t, path)
	now := time.Now()
	token, err := auth.CreateSession(ctx, sqlDB, now)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := adminInit(path, "admin@example.com", fakePrompt("new pw", "new pw")); err != nil {
		t.Fatalf("second adminInit: %v", err)
	}
	if ok, _ := auth.ValidSession(ctx, sqlDB, token, now); ok {
		t.Error("session survived admin init")
	}
	if ok, _ := auth.Authenticate(ctx, sqlDB, "admin@example.com", "old pw"); ok {
		t.Error("old password still works")
	}
	if ok, _ := auth.Authenticate(ctx, sqlDB, "admin@example.com", "new pw"); !ok {
		t.Error("new password does not work")
	}
}
