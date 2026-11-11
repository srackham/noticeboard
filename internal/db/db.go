// Package db opens the SQLite database and applies the schema migrations.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"noticeboard/migrations"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// dsnOptions applies to every connection in the pool. WAL lets the server and
// `admin upsert` use the file at the same time. The busy timeout makes a
// writer wait for the other process instead of failing at once. _txlock=immediate
// takes the write lock when a transaction begins, so two migrators cannot
// interleave.
const dsnOptions = "_pragma=journal_mode(WAL)&_busy_timeout=5000&_txlock=immediate"

// ErrNewerSchema reports a database whose schema version is newer than the
// newest migration this binary knows.
var ErrNewerSchema = errors.New("database schema is newer than this version of noticeboard supports")

// Open opens the SQLite database at path, creating the file if it does not
// exist, and applies any pending migrations. The caller must Close the handle.
func Open(path string) (*sql.DB, error) {
	if strings.Contains(path, "?") {
		// The driver reads its options after the first '?' in the name.
		return nil, fmt.Errorf("database path %q must not contain '?'", path)
	}
	sqlDB, err := sql.Open("sqlite", path+"?"+dsnOptions)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	if err := Migrate(sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	return sqlDB, nil
}

// Migrate applies, in order, each embedded migration whose version is above
// the database's PRAGMA user_version. Each migration runs in its own
// transaction together with the version update. Migrations are forward-only.
func Migrate(sqlDB *sql.DB) error {
	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, m := range migs {
		if err := applyMigration(sqlDB, m, len(migs)); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

// applyMigration runs m unless the database has already reached its version.
// The version is read inside the transaction, so a concurrent migrator that
// got there first is detected rather than repeated.
func applyMigration(sqlDB *sql.DB, m migration, latest int) error {
	tx, err := sqlDB.Begin() // BEGIN IMMEDIATE (see dsnOptions)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after a successful Commit

	var current int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > latest {
		return fmt.Errorf("version %d, newest known is %d: %w", current, latest, ErrNewerSchema)
	}
	if current >= m.version {
		return nil // already applied
	}
	if _, err := tx.Exec(m.sql); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return err
	}
	return tx.Commit()
}

// migration is one embedded schema change.
type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the embedded migration files in version order. It
// returns an error unless the versions run 1, 2, 3, ... with no gaps or
// duplicates.
func loadMigrations() ([]migration, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return nil, err
	}

	migs := make([]migration, 0, len(names))
	for _, name := range names {
		prefix, _, _ := strings.Cut(path.Base(name), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("migration %s: name must start with a version number", name)
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, err
		}
		migs = append(migs, migration{version: version, name: name, sql: string(body)})
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	for i, m := range migs {
		if m.version != i+1 {
			return nil, fmt.Errorf("migrations: expected version %04d, found %s", i+1, m.name)
		}
	}
	return migs, nil
}
