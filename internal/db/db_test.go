package db

import (
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"noticeboard/migrations"
)

// latestVersion is the number of migrations the binary knows.
func latestVersion(t *testing.T) int {
	t.Helper()
	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	return len(migs)
}

func userVersion(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var v int
	if err := sqlDB.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	return v
}

func TestOpenAppliesAllMigrations(t *testing.T) {
	sqlDB, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sqlDB.Close()

	if got, want := userVersion(t, sqlDB), latestVersion(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	sqlDB, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sqlDB.Close()

	if err := Migrate(sqlDB); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if got, want := userVersion(t, sqlDB), latestVersion(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
}

func TestNewerSchemaIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := raw.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	raw.Close()

	if _, err := Open(path); !errors.Is(err, ErrNewerSchema) {
		t.Errorf("Open error = %v, want ErrNewerSchema", err)
	}
}

// TestUpgradeFromVersionOneKeepsAdminAccount checks the migration path for a
// database created before usernames: the email column is renamed, and the
// existing account and its password hash survive.
func TestUpgradeFromVersionOneKeepsAdminAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	// Build a version 1 database by hand, as the first release would have.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	schema, err := fs.ReadFile(migrations.FS, "0001_init.sql")
	if err != nil {
		t.Fatalf("read 0001: %v", err)
	}
	if _, err := raw.Exec(string(schema)); err != nil {
		t.Fatalf("apply 0001: %v", err)
	}
	if _, err := raw.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO admins (id, email, password_hash) VALUES (1, 'joe@example.com', 'hash')`); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	raw.Close()

	sqlDB, err := Open(path)
	if err != nil {
		t.Fatalf("Open after upgrade: %v", err)
	}
	defer sqlDB.Close()

	if got, want := userVersion(t, sqlDB), latestVersion(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
	var username, hash string
	if err := sqlDB.QueryRow(`SELECT username, password_hash FROM admins WHERE id = 1`).Scan(&username, &hash); err != nil {
		t.Fatalf("read admin after upgrade: %v", err)
	}
	if username != "joe@example.com" || hash != "hash" {
		t.Errorf("admin after upgrade = (%q, %q), want the original account", username, hash)
	}
}
