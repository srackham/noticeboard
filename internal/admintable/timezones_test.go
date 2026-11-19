package admintable

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"noticeboard/internal/db"
)

// newTestDB returns a fresh migrated database with the seed data.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

// rowByCell finds the first row whose second cell (the label) is want.
func rowByCell(t *testing.T, p Page, want string) Row {
	t.Helper()
	for _, r := range p.Rows {
		if len(r.Cells) > 1 && r.Cells[1] == want {
			return r
		}
	}
	t.Fatalf("no row labelled %q in page %d", want, p.Number)
	return Row{}
}

func TestTimezonesShape(t *testing.T) {
	tbl := NewTimezones(newTestDB(t))
	if tbl.Slug() != "timezones" || tbl.Title() != "Time zones" {
		t.Errorf("Slug/Title = %q/%q", tbl.Slug(), tbl.Title())
	}
	want := []string{"id", "label", "zone", "enabled", "sort"}
	got := tbl.Columns()
	if len(got) != len(want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("columns = %v, want %v", got, want)
			break
		}
	}

	fields := tbl.Fields()
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	for _, want := range []string{"label", "zone", "enabled", "sort"} {
		if !slices.Contains(names, want) {
			t.Errorf("fields = %v, want %q", names, want)
		}
	}
	if slices.Contains(names, "id") {
		t.Error("the ID column must not be editable")
	}
}

func TestTimezonesDefaults(t *testing.T) {
	tbl := NewTimezones(newTestDB(t))
	values, err := tbl.Defaults(t.Context())
	if err != nil {
		t.Fatalf("Defaults: %v", err)
	}
	if values["enabled"] != "1" {
		t.Errorf("default enabled = %q, want 1", values["enabled"])
	}
	if values["sort"] != "10" {
		t.Errorf("default sort = %q, want 10 (after the nine seed clocks)", values["sort"])
	}
}

func TestTimezonesListShowsRawColumns(t *testing.T) {
	tbl := NewTimezones(newTestDB(t))

	p, err := tbl.List(t.Context(), Paging{Page: 1, Size: 4}, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if p.Number != 1 || p.Pages != 3 || p.Total != 9 || p.Offset != 0 {
		t.Errorf("page = %+v, want page 1 of 3, total 9, offset 0", p)
	}
	if len(p.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(p.Rows))
	}
	utc := rowByCell(t, p, "UTC")
	want := []string{"1", "UTC", "UTC", "1", "1"}
	for i := range want {
		if utc.Cells[i] != want[i] {
			t.Errorf("UTC cells = %v, want %v", utc.Cells, want)
			break
		}
	}

	last, err := tbl.List(t.Context(), Paging{Page: 3, Size: 4}, "")
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(last.Rows) != 1 || last.Offset != 8 {
		t.Errorf("last page = %+v, want one row at offset 8", last)
	}
}

func TestTimezonesListSearch(t *testing.T) {
	tbl := NewTimezones(newTestDB(t))

	p, err := tbl.List(t.Context(), Paging{Page: 1, Size: 20}, "Asia")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if p.Total != 3 || len(p.Rows) != 3 {
		t.Fatalf("search Asia = %d rows, total %d; want 3", len(p.Rows), p.Total)
	}
	want := []string{"3", "India", "Asia/Kolkata", "1", "3"}
	for i := range want {
		if p.Rows[0].Cells[i] != want[i] {
			t.Errorf("first match cells = %v, want %v", p.Rows[0].Cells, want)
			break
		}
	}

	none, err := tbl.List(t.Context(), Paging{Page: 1, Size: 20}, "Nowhere")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if none.Total != 0 || len(none.Rows) != 0 || none.Number != 1 || none.Pages != 1 {
		t.Errorf("no match = %+v, want page 1 of 1 with no rows", none)
	}
}

func TestTimezonesLoad(t *testing.T) {
	tbl := NewTimezones(newTestDB(t))
	p, err := tbl.List(t.Context(), Paging{Page: 1, Size: 5}, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	utc := rowByCell(t, p, "UTC")

	values, err := tbl.Load(t.Context(), utc.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Values{"label": "UTC", "zone": "UTC", "enabled": "1", "sort": "1"}
	for name, v := range want {
		if values[name] != v {
			t.Errorf("Load[%s] = %q, want %q", name, values[name], v)
		}
	}

	if _, err := tbl.Load(t.Context(), 999999); err != ErrNotFound {
		t.Errorf("Load unknown = %v, want ErrNotFound", err)
	}
}

func TestTimezonesCreateAndUpdate(t *testing.T) {
	sqlDB := newTestDB(t)
	tbl := NewTimezones(sqlDB)
	ctx := t.Context()

	// Create a disabled record with an explicit sort, so the nine-enabled
	// limit does not apply.
	values := Values{"label": "Honolulu", "zone": "Pacific/Honolulu", "enabled": "0", "sort": "42"}
	if err := tbl.Create(ctx, values); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var sort int
	var enabled bool
	if err := sqlDB.QueryRow(`SELECT enabled, sort FROM timezones WHERE label = 'Honolulu'`).Scan(&enabled, &sort); err != nil {
		t.Fatalf("read created clock: %v", err)
	}
	if enabled || sort != 42 {
		t.Errorf("created clock = enabled %v, sort %d; want disabled, 42", enabled, sort)
	}

	// Update every editable value. Nine clocks are enabled, so free a place first.
	p, err := tbl.List(ctx, Paging{Page: 1, Size: 20}, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	utc := rowByCell(t, p, "UTC")
	if err := tbl.Update(ctx, utc.ID, Values{"label": "UTC", "zone": "UTC", "enabled": "0", "sort": "1"}); err != nil {
		t.Fatalf("disable UTC: %v", err)
	}
	honolulu := rowByCell(t, p, "Honolulu")
	edit := Values{"label": "Hawaii", "zone": "Pacific/Honolulu", "enabled": "1", "sort": "50"}
	if err := tbl.Update(ctx, honolulu.ID, edit); err != nil {
		t.Fatalf("Update: %v", err)
	}
	var label string
	sort = 0
	if err := sqlDB.QueryRow(`SELECT label, enabled, sort FROM timezones WHERE id = ?`, honolulu.ID).
		Scan(&label, &enabled, &sort); err != nil {
		t.Fatalf("read updated clock: %v", err)
	}
	if label != "Hawaii" || !enabled || sort != 50 {
		t.Errorf("updated clock = %q, enabled %v, sort %d; want Hawaii, enabled, 50", label, enabled, sort)
	}

	if err := tbl.Delete(ctx, honolulu.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := tbl.Delete(ctx, honolulu.ID); err != ErrNotFound {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestTimezonesCreateValidation(t *testing.T) {
	tbl := NewTimezones(newTestDB(t))
	ctx := t.Context()

	tests := []struct {
		name      string
		values    Values
		wantField string
		wantText  string
	}{
		{
			name:      "duplicate label",
			values:    Values{"label": "UTC", "zone": "Europe/London", "enabled": "0", "sort": "42"},
			wantField: "label",
			wantText:  "already used",
		},
		{
			name:      "unknown zone",
			values:    Values{"label": "Mars", "zone": "Mars/Olympus", "enabled": "0", "sort": "42"},
			wantField: "zone",
			wantText:  "Unknown time zone",
		},
		{
			name:      "duplicate sort",
			values:    Values{"label": "Honolulu", "zone": "Pacific/Honolulu", "enabled": "0", "sort": "1"},
			wantField: "sort",
			wantText:  "already used",
		},
		{
			name:      "enabled not 0 or 1",
			values:    Values{"label": "Honolulu", "zone": "Pacific/Honolulu", "enabled": "yes", "sort": "42"},
			wantField: "enabled",
			wantText:  "0 or 1",
		},
		{
			name:      "sort not a number",
			values:    Values{"label": "Honolulu", "zone": "Pacific/Honolulu", "enabled": "0", "sort": "soon"},
			wantField: "sort",
			wantText:  "whole number",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tbl.Create(ctx, tt.values)
			fieldErrs, ok := err.(FieldErrors)
			if !ok {
				t.Fatalf("Create %v = %v, want FieldErrors", tt.values, err)
			}
			if !strings.Contains(fieldErrs[tt.wantField], tt.wantText) {
				t.Errorf("errors = %v, want %q to contain %q", fieldErrs, tt.wantField, tt.wantText)
			}
		})
	}

	// Creating an enabled clock while nine are enabled is refused.
	err := tbl.Create(ctx, Values{"label": "Honolulu", "zone": "Pacific/Honolulu", "enabled": "1", "sort": "42"})
	if _, ok := err.(Refused); !ok {
		t.Errorf("tenth enabled clock = %v, want Refused", err)
	}
}

func TestTimezonesUpdateValidation(t *testing.T) {
	sqlDB := newTestDB(t)
	tbl := NewTimezones(sqlDB)
	ctx := t.Context()

	p, err := tbl.List(ctx, Paging{Page: 1, Size: 20}, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	india := rowByCell(t, p, "India")

	// A sort value in use by another clock is refused.
	err = tbl.Update(ctx, india.ID, Values{"label": "India", "zone": "Asia/Kolkata", "enabled": "1", "sort": "2"})
	fieldErrs, ok := err.(FieldErrors)
	if !ok || !strings.Contains(fieldErrs["sort"], "already used") {
		t.Errorf("clashing sort = %v, want a sort field error", err)
	}

	// Turning on a tenth clock is refused: disable UTC, add an enabled clock,
	// then try to enable UTC again.
	utc := rowByCell(t, p, "UTC")
	if err := tbl.Update(ctx, utc.ID, Values{"label": "UTC", "zone": "UTC", "enabled": "0", "sort": "1"}); err != nil {
		t.Fatalf("disable UTC: %v", err)
	}
	if err := tbl.Create(ctx, Values{"label": "Honolulu", "zone": "Pacific/Honolulu", "enabled": "1", "sort": "42"}); err != nil {
		t.Fatalf("create enabled clock: %v", err)
	}
	err = tbl.Update(ctx, utc.ID, Values{"label": "UTC", "zone": "UTC", "enabled": "1", "sort": "1"})
	if _, ok := err.(Refused); !ok {
		t.Errorf("enabling a tenth clock = %v, want Refused", err)
	}

	// Updating a missing record.
	err = tbl.Update(ctx, 999999, Values{"label": "X", "zone": "UTC", "enabled": "1", "sort": "1"})
	if err != ErrNotFound {
		t.Errorf("Update unknown = %v, want ErrNotFound", err)
	}
}
