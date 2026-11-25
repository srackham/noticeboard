// Package zones is the repository for the clocks in the timezones table
// (DESIGN.md section 3.1). Writes run in transactions. The database uses
// BEGIN IMMEDIATE (see internal/db), so a check such as the nine-clock limit
// and the write that follows it cannot be interleaved with another writer.
package zones

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxClocks is the most clocks that may be enabled. The clocks page is a 3×3 grid.
const MaxClocks = 9

// MaxLabelLength is the longest label, in characters.
const MaxLabelLength = 60

// Field names used as keys in Errors. They match the form field names.
const (
	FieldLabel = "label"
	FieldZone  = "zone"
	FieldSort  = "sort"
)

// Errors maps a field name to a message for the user. It is returned as an error when input is invalid.
type Errors map[string]string

// Error joins the messages in field-name order.
func (e Errors) Error() string {
	fields := make([]string, 0, len(e))
	for f := range e {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	msgs := make([]string, 0, len(e))
	for _, f := range fields {
		msgs = append(msgs, e[f])
	}
	return strings.Join(msgs, " ")
}

// Errors that the callers test with errors.Is.
var (
	// ErrNotFound means no clock has the given ID.
	ErrNotFound = errors.New("clock not found")
	// ErrTooManyClocks means nine clocks are already enabled.
	ErrTooManyClocks = errors.New("Nine clocks are already enabled. Disable one first.")
)

// Direction is the way a clock moves in the list.
type Direction string

// The two directions accepted by Move.
const (
	Up   Direction = "up"
	Down Direction = "down"
)

// ParseDirection converts the dir query value to a Direction.
func ParseDirection(s string) (Direction, bool) {
	switch d := Direction(s); d {
	case Up, Down:
		return d, true
	}
	return "", false
}

// Zone is one clock.
type Zone struct {
	ID      int64
	Label   string
	Zone    string // IANA zone ID, such as "Australia/Sydney"
	Enabled bool
	Sort    int
}

// queryer is implemented by *sql.DB and *sql.Tx, so reads can run inside a transaction.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// validate trims label and zone and checks them. It does not check uniqueness or the limit.
func validate(label, zone string) (string, string, Errors) {
	label = strings.TrimSpace(label)
	zone = strings.TrimSpace(zone)
	errs := Errors{}

	switch n := utf8.RuneCountInString(label); {
	case n == 0:
		errs[FieldLabel] = "Label is required."
	case n > MaxLabelLength:
		errs[FieldLabel] = fmt.Sprintf("Label must be %d characters or fewer.", MaxLabelLength)
	}

	switch {
	case zone == "":
		errs[FieldZone] = "Zone is required."
	case zone == "Local":
		// time.LoadLocation accepts "Local", but it is not a fixed zone.
		errs[FieldZone] = "Local is not a fixed zone. Choose a named zone such as Europe/London."
	default:
		if _, err := time.LoadLocation(zone); err != nil {
			errs[FieldZone] = "Unknown time zone. Use an IANA name such as Australia/Sydney."
		}
	}
	return label, zone, errs
}

// checkUnique reports a duplicate label or zone held by a clock other than id.
// Pass 0 for id when creating. It returns nil Errors when there are no duplicates.
func checkUnique(ctx context.Context, q queryer, id int64, label, zone string) (Errors, error) {
	errs := Errors{}
	var n int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM timezones WHERE label = ? AND id <> ?`, label, id).Scan(&n); err != nil {
		return nil, fmt.Errorf("check label: %w", err)
	}
	if n > 0 {
		errs[FieldLabel] = "That label is already used by another clock."
	}
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM timezones WHERE zone = ? AND id <> ?`, zone, id).Scan(&n); err != nil {
		return nil, fmt.Errorf("check zone: %w", err)
	}
	if n > 0 {
		errs[FieldZone] = "That time zone is already on the clocks page."
	}
	if len(errs) == 0 {
		return nil, nil
	}
	return errs, nil
}

// countEnabled returns how many clocks are enabled.
func countEnabled(ctx context.Context, q queryer) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM timezones WHERE enabled = 1`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count enabled clocks: %w", err)
	}
	return n, nil
}

// get returns the clock with the given ID, or ErrNotFound.
func get(ctx context.Context, q queryer, id int64) (Zone, error) {
	var z Zone
	err := q.QueryRowContext(ctx,
		`SELECT id, label, zone, enabled, sort FROM timezones WHERE id = ?`, id).
		Scan(&z.ID, &z.Label, &z.Zone, &z.Enabled, &z.Sort)
	if errors.Is(err, sql.ErrNoRows) {
		return Zone{}, ErrNotFound
	}
	if err != nil {
		return Zone{}, fmt.Errorf("read clock: %w", err)
	}
	return z, nil
}

// withTx runs fn in a transaction, which is committed if fn returns nil. Errors
// from fn are returned unchanged, so callers can test them.
func withTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() // no-op after a successful Commit

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ListEnabled returns the enabled clocks in display order.
func ListEnabled(ctx context.Context, db *sql.DB) ([]Zone, error) {
	return query(ctx, db, `SELECT id, label, zone, enabled, sort FROM timezones WHERE enabled = 1 ORDER BY sort`)
}

// List returns every clock, enabled or not, in display order.
func List(ctx context.Context, db *sql.DB) ([]Zone, error) {
	return query(ctx, db, `SELECT id, label, zone, enabled, sort FROM timezones ORDER BY sort`)
}

// CountEnabled returns how many clocks are enabled.
func CountEnabled(ctx context.Context, db *sql.DB) (int, error) {
	return countEnabled(ctx, db)
}

// Get returns the clock with the given ID, or ErrNotFound.
func Get(ctx context.Context, db *sql.DB, id int64) (Zone, error) {
	return get(ctx, db, id)
}

func query(ctx context.Context, q queryer, sqlText string, args ...any) ([]Zone, error) {
	rows, err := q.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("list clocks: %w", err)
	}
	defer rows.Close()

	var zs []Zone
	for rows.Next() {
		var z Zone
		if err := rows.Scan(&z.ID, &z.Label, &z.Zone, &z.Enabled, &z.Sort); err != nil {
			return nil, fmt.Errorf("list clocks: %w", err)
		}
		zs = append(zs, z)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list clocks: %w", err)
	}
	return zs, nil
}

// Values are a clock's editable column values.
type Values struct {
	Label   string
	Zone    string
	Enabled bool
	Sort    int
}

// NextSort returns the sort value a new clock takes when it is appended: the
// largest existing sort plus one.
func NextSort(ctx context.Context, db *sql.DB) (int, error) {
	var maxSort int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sort), 0) FROM timezones`).Scan(&maxSort); err != nil {
		return 0, fmt.Errorf("read sort: %w", err)
	}
	return maxSort + 1, nil
}

// checkSortUnique reports a duplicate sort value held by a clock other than id.
// Pass 0 for id when creating. It returns nil Errors when the value is free.
func checkSortUnique(ctx context.Context, q queryer, id int64, sort int) (Errors, error) {
	var n int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM timezones WHERE sort = ? AND id <> ?`, sort, id).Scan(&n); err != nil {
		return nil, fmt.Errorf("check sort: %w", err)
	}
	if n > 0 {
		return Errors{FieldSort: "That sort value is already used by another clock."}, nil
	}
	return nil, nil
}

// Create adds an enabled clock at the end of the list. It returns Errors for
// invalid or duplicate input, and ErrTooManyClocks when nine clocks are already enabled.
func Create(ctx context.Context, db *sql.DB, label, zone string) (Zone, error) {
	return create(ctx, db, label, zone, true, nil)
}

// CreateWith adds a clock with explicit enabled and sort values. It returns
// Errors for invalid, duplicate or clashing input, and ErrTooManyClocks when an
// enabled clock is created while nine are already enabled.
func CreateWith(ctx context.Context, db *sql.DB, v Values) (Zone, error) {
	sort := v.Sort
	return create(ctx, db, v.Label, v.Zone, v.Enabled, &sort)
}

// create inserts a clock. A nil sort appends it after the last clock; an
// explicit sort must be unique.
func create(ctx context.Context, db *sql.DB, label, zone string, enabled bool, sort *int) (Zone, error) {
	label, zone, errs := validate(label, zone)
	if len(errs) > 0 {
		return Zone{}, errs
	}

	var z Zone
	err := withTx(ctx, db, func(tx *sql.Tx) error {
		dup, err := checkUnique(ctx, tx, 0, label, zone)
		if err != nil {
			return err
		}
		if len(dup) > 0 {
			return dup
		}
		if enabled {
			n, err := countEnabled(ctx, tx)
			if err != nil {
				return err
			}
			if n >= MaxClocks {
				return ErrTooManyClocks
			}
		}

		var sortValue int
		if sort == nil {
			var maxSort int
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(sort), 0) FROM timezones`).Scan(&maxSort); err != nil {
				return fmt.Errorf("read sort: %w", err)
			}
			sortValue = maxSort + 1
		} else {
			clash, err := checkSortUnique(ctx, tx, 0, *sort)
			if err != nil {
				return err
			}
			if len(clash) > 0 {
				return clash
			}
			sortValue = *sort
		}

		res, err := tx.ExecContext(ctx,
			`INSERT INTO timezones (label, zone, enabled, sort) VALUES (?, ?, ?, ?)`,
			label, zone, enabled, sortValue)
		if err != nil {
			return fmt.Errorf("insert clock: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("insert clock: %w", err)
		}
		z = Zone{ID: id, Label: label, Zone: zone, Enabled: enabled, Sort: sortValue}
		return nil
	})
	if err != nil {
		return Zone{}, err
	}
	return z, nil
}

// Update changes the label and zone of a clock. It leaves its enabled state and position unchanged.
func Update(ctx context.Context, db *sql.DB, id int64, label, zone string) (Zone, error) {
	return update(ctx, db, id, label, zone, nil, nil)
}

// UpdateWith changes every editable value of a clock. Enabling is refused with
// ErrTooManyClocks when nine clocks are already enabled, and a sort value in use by
// another clock is refused with Errors.
func UpdateWith(ctx context.Context, db *sql.DB, id int64, v Values) (Zone, error) {
	enabled, sort := v.Enabled, v.Sort
	return update(ctx, db, id, v.Label, v.Zone, &enabled, &sort)
}

// update changes a clock. A nil enabled or sort leaves that value unchanged.
func update(ctx context.Context, db *sql.DB, id int64, label, zone string, enabled *bool, sort *int) (Zone, error) {
	var z Zone
	err := withTx(ctx, db, func(tx *sql.Tx) error {
		cur, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		label, zone, errs := validate(label, zone)
		if len(errs) > 0 {
			return errs
		}
		dup, err := checkUnique(ctx, tx, id, label, zone)
		if err != nil {
			return err
		}
		if len(dup) > 0 {
			return dup
		}

		wantEnabled := cur.Enabled
		if enabled != nil {
			wantEnabled = *enabled
		}
		if wantEnabled && !cur.Enabled {
			n, err := countEnabled(ctx, tx)
			if err != nil {
				return err
			}
			if n >= MaxClocks {
				return ErrTooManyClocks
			}
		}

		wantSort := cur.Sort
		if sort != nil && *sort != cur.Sort {
			clash, err := checkSortUnique(ctx, tx, id, *sort)
			if err != nil {
				return err
			}
			if len(clash) > 0 {
				return clash
			}
			wantSort = *sort
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE timezones SET label = ?, zone = ?, enabled = ?, sort = ? WHERE id = ?`,
			label, zone, wantEnabled, wantSort, id); err != nil {
			return fmt.Errorf("update clock: %w", err)
		}
		cur.Label, cur.Zone, cur.Enabled, cur.Sort = label, zone, wantEnabled, wantSort
		z = cur
		return nil
	})
	if err != nil {
		return Zone{}, err
	}
	return z, nil
}

// Toggle flips a clock between enabled and disabled. Enabling is refused with
// ErrTooManyClocks when nine clocks are already enabled.
func Toggle(ctx context.Context, db *sql.DB, id int64) error {
	return withTx(ctx, db, func(tx *sql.Tx) error {
		cur, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.Enabled {
			_, err := tx.ExecContext(ctx, `UPDATE timezones SET enabled = 0 WHERE id = ?`, id)
			if err != nil {
				return fmt.Errorf("disable clock: %w", err)
			}
			return nil
		}
		enabled, err := countEnabled(ctx, tx)
		if err != nil {
			return err
		}
		if enabled >= MaxClocks {
			return ErrTooManyClocks
		}
		if _, err := tx.ExecContext(ctx, `UPDATE timezones SET enabled = 1 WHERE id = ?`, id); err != nil {
			return fmt.Errorf("enable clock: %w", err)
		}
		return nil
	})
}

// Move swaps a clock's position with its neighbour in the list. The neighbour may be
// enabled or disabled. Moving the first clock up or the last clock down does nothing.
func Move(ctx context.Context, db *sql.DB, id int64, dir Direction) error {
	var next string
	switch dir {
	case Up:
		next = `SELECT id, sort FROM timezones WHERE sort < ? ORDER BY sort DESC LIMIT 1`
	case Down:
		next = `SELECT id, sort FROM timezones WHERE sort > ? ORDER BY sort ASC LIMIT 1`
	default:
		return fmt.Errorf("move: invalid direction %q", dir)
	}

	return withTx(ctx, db, func(tx *sql.Tx) error {
		cur, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		var neighbourID int64
		var neighbourSort int
		err = tx.QueryRowContext(ctx, next, cur.Sort).Scan(&neighbourID, &neighbourSort)
		if errors.Is(err, sql.ErrNoRows) {
			return nil // already first (up) or last (down)
		}
		if err != nil {
			return fmt.Errorf("find neighbour: %w", err)
		}

		// sort is unique, so the clock is first moved to a value no clock has. The swap follows.
		if _, err := tx.ExecContext(ctx,
			`UPDATE timezones SET sort = (SELECT MAX(sort) FROM timezones) + 1 WHERE id = ?`, id); err != nil {
			return fmt.Errorf("move clock: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE timezones SET sort = ? WHERE id = ?`, cur.Sort, neighbourID); err != nil {
			return fmt.Errorf("move clock: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE timezones SET sort = ? WHERE id = ?`, neighbourSort, id); err != nil {
			return fmt.Errorf("move clock: %w", err)
		}
		return nil
	})
}

// Delete removes a clock. It returns ErrNotFound if no clock has the ID.
func Delete(ctx context.Context, db *sql.DB, id int64) error {
	res, err := db.ExecContext(ctx, `DELETE FROM timezones WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete clock: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete clock: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
