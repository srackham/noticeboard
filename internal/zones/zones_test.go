package zones

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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

// idOf returns the ID of the seeded clock with the given zone.
func idOf(t *testing.T, sqlDB *sql.DB, zone string) int64 {
	t.Helper()
	var id int64
	if err := sqlDB.QueryRow(`SELECT id FROM timezones WHERE zone = ?`, zone).Scan(&id); err != nil {
		t.Fatalf("id of %s: %v", zone, err)
	}
	return id
}

// labelsOf returns the labels of zs in order.
func labelsOf(zs []Zone) []string {
	out := make([]string, len(zs))
	for i, z := range zs {
		out[i] = z.Label
	}
	return out
}

// errorFields reports the field names in err, which must be an Errors value.
func errorFields(t *testing.T, err error) []string {
	t.Helper()
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("error = %v, want Errors", err)
	}
	var fields []string
	for f := range errs {
		fields = append(fields, f)
	}
	return fields
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		label, zone string
		wantLabel   string
		wantZone    string
		wantFields  []string
	}{
		{name: "trims label and zone", label: "  Harbour  ", zone: "  Australia/Sydney ", wantLabel: "Harbour", wantZone: "Australia/Sydney"},
		{name: "UTC is allowed", label: "UTC", zone: "UTC", wantLabel: "UTC", wantZone: "UTC"},
		{name: "Etc/UTC is allowed", label: "Etc", zone: "Etc/UTC", wantLabel: "Etc", wantZone: "Etc/UTC"},
		{name: "blank label", label: "   ", zone: "UTC", wantFields: []string{FieldLabel}},
		{name: "60 characters", label: strings.Repeat("a", 60), zone: "UTC", wantLabel: strings.Repeat("a", 60), wantZone: "UTC"},
		{name: "61 characters", label: strings.Repeat("a", 61), zone: "UTC", wantFields: []string{FieldLabel}},
		{name: "60 multibyte characters", label: strings.Repeat("é", 60), zone: "UTC", wantLabel: strings.Repeat("é", 60), wantZone: "UTC"},
		{name: "empty zone", label: "x", zone: "", wantFields: []string{FieldZone}},
		{name: "blank zone", label: "x", zone: "   ", wantFields: []string{FieldZone}},
		{name: "Local is rejected", label: "x", zone: "Local", wantFields: []string{FieldZone}},
		{name: "unknown zone", label: "x", zone: "Not/AZone", wantFields: []string{FieldZone}},
		{name: "blank label and bad zone", label: "", zone: "Not/AZone", wantFields: []string{FieldLabel, FieldZone}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLabel, gotZone, errs := validate(tt.label, tt.zone)
			if len(tt.wantFields) > 0 {
				if len(errs) != len(tt.wantFields) {
					t.Fatalf("errors = %v, want fields %v", errs, tt.wantFields)
				}
				for _, f := range tt.wantFields {
					if _, ok := errs[f]; !ok {
						t.Errorf("missing error for %s: %v", f, errs)
					}
				}
				return
			}
			if len(errs) != 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			if gotLabel != tt.wantLabel || gotZone != tt.wantZone {
				t.Errorf("validate = (%q, %q), want (%q, %q)", gotLabel, gotZone, tt.wantLabel, tt.wantZone)
			}
		})
	}
}

func TestListReturnsSeedClocksInOrder(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)

	all, err := List(ctx, sqlDB)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"UTC", "New Zealand", "India", "Melbourne/Sydney", "China", "Canada", "Philippines", "Russia", "Ukraine"}
	if got := labelsOf(all); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("List labels = %v, want %v", got, want)
	}
	enabled, err := ListEnabled(ctx, sqlDB)
	if err != nil || len(enabled) != 9 {
		t.Errorf("ListEnabled = %d clocks (%v), want 9", len(enabled), err)
	}
}

func TestCreateAddsClockAtEndWhenRoomAvailable(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	if err := Toggle(ctx, sqlDB, idOf(t, sqlDB, "Europe/Kyiv")); err != nil {
		t.Fatalf("disable: %v", err)
	}

	z, err := Create(ctx, sqlDB, "  Harbour ", " Asia/Tokyo ")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if z.Label != "Harbour" || z.Zone != "Asia/Tokyo" || !z.Enabled || z.Sort != 10 {
		t.Errorf("Create = %+v, want trimmed, enabled, sort 10", z)
	}
}

func TestCreateRefusedWhenNineEnabled(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)

	_, err := Create(ctx, sqlDB, "Harbour", "Asia/Tokyo")
	if !errors.Is(err, ErrTooManyClocks) {
		t.Errorf("Create with nine enabled: error = %v, want ErrTooManyClocks", err)
	}
	all, _ := List(ctx, sqlDB)
	if len(all) != 9 {
		t.Errorf("clocks = %d after refused create, want 9", len(all))
	}
}

func TestCreateRejectsDuplicates(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	if err := Toggle(ctx, sqlDB, idOf(t, sqlDB, "Europe/Kyiv")); err != nil {
		t.Fatalf("disable: %v", err)
	}

	_, err := Create(ctx, sqlDB, "UTC", "Asia/Tokyo")
	if fields := errorFields(t, err); len(fields) != 1 || fields[0] != FieldLabel {
		t.Errorf("duplicate label: fields = %v, want [label]", fields)
	}

	_, err = Create(ctx, sqlDB, "Tokyo", "Asia/Kolkata")
	if fields := errorFields(t, err); len(fields) != 1 || fields[0] != FieldZone {
		t.Errorf("duplicate zone: fields = %v, want [zone]", fields)
	}
}

func TestToggleDisablesAlwaysAndEnablesWhenRoom(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	id := idOf(t, sqlDB, "Asia/Kolkata")

	if err := Toggle(ctx, sqlDB, id); err != nil {
		t.Fatalf("disable with nine enabled: %v", err)
	}
	if err := Toggle(ctx, sqlDB, id); err != nil {
		t.Fatalf("enable with eight enabled: %v", err)
	}
}

func TestToggleEnableRefusedWhenNineEnabled(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	india := idOf(t, sqlDB, "Asia/Kolkata")
	if err := Toggle(ctx, sqlDB, india); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := Create(ctx, sqlDB, "Harbour", "Asia/Tokyo"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := Toggle(ctx, sqlDB, india); !errors.Is(err, ErrTooManyClocks) {
		t.Errorf("enable at nine: error = %v, want ErrTooManyClocks", err)
	}
	var enabled int
	sqlDB.QueryRow(`SELECT enabled FROM timezones WHERE id = ?`, india).Scan(&enabled)
	if enabled != 0 {
		t.Error("refused enable changed the clock")
	}
}

func TestMoveSwapsWithAdjacentClock(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	india := idOf(t, sqlDB, "Asia/Kolkata")

	if err := Move(ctx, sqlDB, india, Up); err != nil {
		t.Fatalf("Move up: %v", err)
	}
	all, _ := List(ctx, sqlDB)
	if got := labelsOf(all)[:3]; strings.Join(got, "|") != "UTC|India|New Zealand" {
		t.Errorf("order after moving India up = %v", got)
	}

	if err := Move(ctx, sqlDB, india, Down); err != nil {
		t.Fatalf("Move down: %v", err)
	}
	all, _ = List(ctx, sqlDB)
	if got := labelsOf(all)[:3]; strings.Join(got, "|") != "UTC|New Zealand|India" {
		t.Errorf("order after moving India down = %v", got)
	}
}

func TestMoveAtEndsIsNoOp(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	before, _ := List(ctx, sqlDB)

	if err := Move(ctx, sqlDB, idOf(t, sqlDB, "UTC"), Up); err != nil {
		t.Fatalf("Move first clock up: %v", err)
	}
	if err := Move(ctx, sqlDB, idOf(t, sqlDB, "Europe/Kyiv"), Down); err != nil {
		t.Fatalf("Move last clock down: %v", err)
	}
	after, _ := List(ctx, sqlDB)
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("clock %d changed: %+v -> %+v", i, before[i], after[i])
		}
	}
}

func TestMoveNeighbourCanBeDisabled(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	nz := idOf(t, sqlDB, "Pacific/Auckland")
	if err := Toggle(ctx, sqlDB, nz); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if err := Move(ctx, sqlDB, idOf(t, sqlDB, "UTC"), Down); err != nil {
		t.Fatalf("Move: %v", err)
	}
	all, _ := List(ctx, sqlDB)
	if got := labelsOf(all)[:2]; strings.Join(got, "|") != "New Zealand|UTC" {
		t.Errorf("order = %v, want the disabled clock swapped with UTC", got)
	}
}

func TestMoveUnknownClock(t *testing.T) {
	if err := Move(context.Background(), openTestDB(t), 999, Up); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestUpdateChangesLabelAndZoneOnly(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	id := idOf(t, sqlDB, "Asia/Kolkata")
	before, _ := Get(ctx, sqlDB, id)

	z, err := Update(ctx, sqlDB, id, "  Mumbai ", "Asia/Kolkata")
	if err != nil {
		t.Fatalf("Update keeping its own zone: %v", err)
	}
	if z.Label != "Mumbai" || z.Enabled != before.Enabled || z.Sort != before.Sort {
		t.Errorf("Update = %+v, want label changed and enabled/sort kept (was %+v)", z, before)
	}
}

func TestUpdateRejectsDuplicateOfAnotherClock(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)

	_, err := Update(ctx, sqlDB, idOf(t, sqlDB, "Asia/Kolkata"), "UTC", "Asia/Kolkata")
	if fields := errorFields(t, err); len(fields) != 1 || fields[0] != FieldLabel {
		t.Errorf("fields = %v, want [label]", fields)
	}
	_, err = Update(ctx, sqlDB, idOf(t, sqlDB, "Asia/Kolkata"), "India", "UTC")
	if fields := errorFields(t, err); len(fields) != 1 || fields[0] != FieldZone {
		t.Errorf("fields = %v, want [zone]", fields)
	}
}

func TestUpdateUnknownClock(t *testing.T) {
	_, err := Update(context.Background(), openTestDB(t), 999, "x", "UTC")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	id := idOf(t, sqlDB, "Asia/Kolkata")

	if err := Delete(ctx, sqlDB, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if all, _ := List(ctx, sqlDB); len(all) != 8 {
		t.Errorf("clocks after delete = %d, want 8", len(all))
	}
	if err := Delete(ctx, sqlDB, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete: error = %v, want ErrNotFound", err)
	}
}

func TestNextSort(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	next, err := NextSort(ctx, sqlDB)
	if err != nil {
		t.Fatalf("NextSort: %v", err)
	}
	if next != 10 {
		t.Errorf("NextSort = %d, want 10", next)
	}
}

func TestCreateWithExplicitValues(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)

	z, err := CreateWith(ctx, sqlDB, Values{Label: "Honolulu", Zone: "Pacific/Honolulu", Enabled: false, Sort: 42})
	if err != nil {
		t.Fatalf("CreateWith: %v", err)
	}
	if z.Enabled || z.Sort != 42 {
		t.Errorf("created = %+v, want disabled with sort 42", z)
	}

	// A sort value in use by another clock is refused.
	_, err = CreateWith(ctx, sqlDB, Values{Label: "Suva", Zone: "Pacific/Fiji", Enabled: false, Sort: 1})
	if fields := errorFields(t, err); len(fields) != 1 || fields[0] != FieldSort {
		t.Errorf("clashing sort fields = %v, want [sort]", fields)
	}

	// An enabled clock is refused when nine are already enabled.
	_, err = CreateWith(ctx, sqlDB, Values{Label: "Suva", Zone: "Pacific/Fiji", Enabled: true, Sort: 43})
	if !errors.Is(err, ErrTooManyClocks) {
		t.Errorf("tenth enabled clock = %v, want ErrTooManyClocks", err)
	}

	// A disabled clock may be added even when the clock limit is reached.
	if _, err := CreateWith(ctx, sqlDB, Values{Label: "Suva", Zone: "Pacific/Fiji", Enabled: false, Sort: 43}); err != nil {
		t.Errorf("disabled clock with the clock limit reached: %v", err)
	}
}

func TestUpdateWithExplicitValues(t *testing.T) {
	ctx := context.Background()
	sqlDB := openTestDB(t)
	id := idOf(t, sqlDB, "Asia/Kolkata")

	// Free a place, then change every value.
	if err := Toggle(ctx, sqlDB, idOf(t, sqlDB, "Europe/Moscow")); err != nil {
		t.Fatalf("disable Moscow: %v", err)
	}
	z, err := UpdateWith(ctx, sqlDB, id, Values{Label: "Delhi", Zone: "Asia/Kolkata", Enabled: true, Sort: 77})
	if err != nil {
		t.Fatalf("UpdateWith: %v", err)
	}
	if z.Label != "Delhi" || z.Sort != 77 || !z.Enabled {
		t.Errorf("updated = %+v, want Delhi, sort 77, enabled", z)
	}

	// A sort value in use is refused.
	_, err = UpdateWith(ctx, sqlDB, id, Values{Label: "Delhi", Zone: "Asia/Kolkata", Enabled: true, Sort: 2})
	if fields := errorFields(t, err); len(fields) != 1 || fields[0] != FieldSort {
		t.Errorf("clashing sort fields = %v, want [sort]", fields)
	}

	// Enabling a tenth clock is refused. Disable UTC and add an enabled clock,
	// then try to enable UTC.
	utc := idOf(t, sqlDB, "UTC")
	if _, err := UpdateWith(ctx, sqlDB, utc, Values{Label: "UTC", Zone: "UTC", Enabled: false, Sort: 1}); err != nil {
		t.Fatalf("disable UTC: %v", err)
	}
	for _, c := range []Values{
		{Label: "Honolulu", Zone: "Pacific/Honolulu", Enabled: true, Sort: 42},
		{Label: "Suva", Zone: "Pacific/Fiji", Enabled: true, Sort: 43},
	} {
		if _, err := CreateWith(ctx, sqlDB, c); err != nil {
			t.Fatalf("create enabled clock %s: %v", c.Label, err)
		}
	}
	_, err = UpdateWith(ctx, sqlDB, utc, Values{Label: "UTC", Zone: "UTC", Enabled: true, Sort: 1})
	if !errors.Is(err, ErrTooManyClocks) {
		t.Errorf("enabling a tenth clock = %v, want ErrTooManyClocks", err)
	}
}
