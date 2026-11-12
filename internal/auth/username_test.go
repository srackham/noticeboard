package auth

import (
	"context"
	"errors"
	"testing"
)

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		valid    bool
	}{
		{name: "plain word", username: "alice", valid: true},
		{name: "email address still works", username: "joe@example.com", valid: true},
		{name: "punctuation and digits", username: "user_01.a-b@c", valid: true},
		{name: "non-ASCII letters", username: "ünïcode", valid: true},
		{name: "one character", username: "x", valid: true},
		{name: "empty", username: "", valid: false},
		{name: "space inside", username: "with space", valid: false},
		{name: "tab inside", username: "tab\there", valid: false},
		{name: "line break", username: "new\nline", valid: false},
		{name: "no-break space", username: "nbsp\u00a0x", valid: false},
		{name: "zero-width space", username: "zero\u200bwidth", valid: false},
		{name: "control character", username: "bell\x07", valid: false},
		{name: "delete character", username: "del\x7f", valid: false},
		{name: "invalid UTF-8", username: "bad\xff", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			if tt.valid && err != nil {
				t.Errorf("ValidateUsername(%q) = %v, want nil", tt.username, err)
			}
			if !tt.valid && !errors.Is(err, ErrInvalidUsername) {
				t.Errorf("ValidateUsername(%q) = %v, want ErrInvalidUsername", tt.username, err)
			}
		})
	}
}

func TestSurroundingSpaceIsTrimmedButInnerSpaceIsRejected(t *testing.T) {
	if got := NormaliseUsername("  alice  "); got != "alice" {
		t.Errorf("NormaliseUsername = %q, want alice", got)
	}
	if err := ValidateUsername(NormaliseUsername("two words")); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("two words: %v, want ErrInvalidUsername", err)
	}
}

func TestAuthenticateWithSpacedUsernameFailsWithoutError(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	if err := UpsertAdmin(ctx, database, "alice", "pw"); err != nil {
		t.Fatalf("UpsertAdmin: %v", err)
	}
	ok, err := Authenticate(ctx, database, "al ice", "pw")
	if err != nil || ok {
		t.Errorf("Authenticate with a spaced username = %v, %v; want false, nil", ok, err)
	}
}

func TestUsernameIsCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	if err := UpsertAdmin(ctx, database, "Alice", "pw"); err != nil {
		t.Fatalf("UpsertAdmin: %v", err)
	}

	var stored string
	if err := database.QueryRow(`SELECT username FROM admins`).Scan(&stored); err != nil {
		t.Fatalf("read admin: %v", err)
	}
	if stored != "alice" {
		t.Errorf("stored username = %q, want the lower-cased form", stored)
	}
	if ok, _ := Authenticate(ctx, database, "ALICE", "pw"); !ok {
		t.Error("username lookup is case-sensitive")
	}
}
