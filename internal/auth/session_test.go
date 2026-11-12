package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
)

var base = time.Date(2024, time.June, 1, 12, 0, 0, 0, time.UTC)

func TestSessionLifetimeIsTwelveHours(t *testing.T) {
	if SessionLifetime != 12*time.Hour {
		t.Errorf("SessionLifetime = %v, want 12h", SessionLifetime)
	}
}

func TestNewSessionToken(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)

	a, err := CreateSession(ctx, database, base)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	b, err := CreateSession(ctx, database, base)
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

	token, err := CreateSession(ctx, database, base)
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

func TestValidSessionExpiry(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		after time.Duration
		want  bool
	}{
		{name: "immediately", after: 0, want: true},
		{name: "one second before expiry", after: SessionLifetime - time.Second, want: true},
		{name: "at expiry", after: SessionLifetime, want: false},
		{name: "after expiry", after: SessionLifetime + time.Hour, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := openTestDB(t)
			token, err := CreateSession(ctx, database, base)
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			got, err := ValidSession(ctx, database, token, base.Add(tt.after))
			if err != nil {
				t.Fatalf("ValidSession: %v", err)
			}
			if got != tt.want {
				t.Errorf("ValidSession after %v = %v, want %v", tt.after, got, tt.want)
			}
		})
	}
}

func TestValidSessionRejectsEmptyAndUnknownTokens(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	if _, err := CreateSession(ctx, database, base); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for _, token := range []string{"", "not-a-session"} {
		if ok, err := ValidSession(ctx, database, token, base); err != nil || ok {
			t.Errorf("ValidSession(%q) = %v, %v; want false, nil", token, ok, err)
		}
	}
}

func TestDeleteSession(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)
	token, err := CreateSession(ctx, database, base)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := DeleteSession(ctx, database, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if ok, _ := ValidSession(ctx, database, token, base); ok {
		t.Error("session still valid after DeleteSession")
	}
	if err := DeleteSession(ctx, database, ""); err != nil {
		t.Errorf("DeleteSession(\"\") = %v, want nil", err)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	ctx := context.Background()
	database := openTestDB(t)

	expired, err := CreateSession(ctx, database, base.Add(-SessionLifetime-time.Hour))
	if err != nil {
		t.Fatalf("CreateSession (expired): %v", err)
	}
	live, err := CreateSession(ctx, database, base)
	if err != nil {
		t.Fatalf("CreateSession (live): %v", err)
	}

	if err := DeleteExpiredSessions(ctx, database, base); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}

	var rows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("session rows = %d (%v), want 1", rows, err)
	}
	if ok, _ := ValidSession(ctx, database, expired, base); ok {
		t.Error("expired session survived cleanup")
	}
	if ok, _ := ValidSession(ctx, database, live, base); !ok {
		t.Error("live session was deleted")
	}
}
