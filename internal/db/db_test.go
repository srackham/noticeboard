package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
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

// TestAdminTableHasUsernameNotEmail checks the schema the application expects.
func TestAdminTableHasUsernameNotEmail(t *testing.T) {
	sqlDB, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sqlDB.Close()

	rows, err := sqlDB.Query(`PRAGMA table_info(admins)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[name] = true
	}
	if !columns["username"] || !columns["password_hash"] {
		t.Errorf("admins columns = %v, want username and password_hash", columns)
	}
	if columns["email"] {
		t.Error("admins still has an email column")
	}
}
