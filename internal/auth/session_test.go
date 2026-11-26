package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestNewSessionToken(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)

	a, err := CreateSession(ctx, database)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	b, err := CreateSession(ctx, database)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if a == b {
		t.Error("two sessions share a token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(a)
	if err != nil || len(raw) != 32 {
		t.Errorf("token %q: decoded %d bytes (%v), want 32 bytes of base64url", a, len(raw), err)
	}
}

func TestSessionIsStoredAsHash(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)

	token, err := CreateSession(ctx, database)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	var stored string
	if err := database.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if stored == token {
		t.Error("database holds the raw token")
	}
	if stored != TokenHash(token) {
		t.Errorf("stored hash = %q, want %q", stored, TokenHash(token))
	}
	if _, err := hex.DecodeString(stored); err != nil || len(stored) != 64 {
		t.Errorf("stored hash %q is not 64 hex characters", stored)
	}
}

func TestValidSessionRejectsEmptyAndUnknownTokens(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	if _, err := CreateSession(ctx, database); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for _, token := range []string{"", "not-a-session"} {
		if ok, err := ValidSession(ctx, database, token); err != nil || ok {
			t.Errorf("ValidSession(%q) = %v, %v; want false, nil", token, ok, err)
		}
	}
}

func TestDeleteSession(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	token, err := CreateSession(ctx, database)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := DeleteSession(ctx, database, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if ok, _ := ValidSession(ctx, database, token); ok {
		t.Error("session still valid after DeleteSession")
	}
	if err := DeleteSession(ctx, database, ""); err != nil {
		t.Errorf("DeleteSession(\"\") = %v, want nil", err)
	}
}
