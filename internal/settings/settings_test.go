package settings

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"noticeboard/internal/db"
)

func validSettings() Settings {
	return Settings{
		Title:            "World Clocks",
		RefreshSeconds:   5,
		ColourBackground: "#d9d9d9",
		ColourLabel:      "#4169e1",
		ColourClock:      "#228b22",
		ColourUTC:        "#b22222",
		TablePageSize:    20,
	}
}

func TestValidateTitle(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		want    string
		wantErr bool
	}{
		{name: "trimmed", title: "  Harbour  ", want: "Harbour"},
		{name: "blank is allowed and hides the header", title: "", want: ""},
		{name: "spaces only become blank", title: "   ", want: ""},
		{name: "80 characters", title: strings.Repeat("a", 80), want: strings.Repeat("a", 80)},
		{name: "80 multibyte characters", title: strings.Repeat("é", 80), want: strings.Repeat("é", 80)},
		{name: "81 characters", title: strings.Repeat("a", 81), wantErr: true},
		{name: "81 characters after trimming", title: "  " + strings.Repeat("a", 81) + "  ", wantErr: true},
		{name: "spaces do not count towards the limit", title: strings.Repeat("a", 80) + "   ", want: strings.Repeat("a", 80)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validSettings()
			in.Title = tt.title
			got, errs := Validate(in)
			if tt.wantErr {
				if _, ok := errs["title"]; !ok {
					t.Fatalf("Validate(title %q): want title error, got %v", tt.title, errs)
				}
				return
			}
			if len(errs) != 0 {
				t.Fatalf("Validate(title %q): unexpected errors %v", tt.title, errs)
			}
			if got.Title != tt.want {
				t.Errorf("Title = %q, want %q", got.Title, tt.want)
			}
		})
	}
}

func TestValidateRefresh(t *testing.T) {
	tests := []struct {
		seconds int
		wantErr bool
	}{
		{seconds: -1, wantErr: true},
		{seconds: 0, wantErr: true},
		{seconds: 1},
		{seconds: 5},
		{seconds: 60},
		{seconds: 61, wantErr: true},
	}
	for _, tt := range tests {
		in := validSettings()
		in.RefreshSeconds = tt.seconds
		_, errs := Validate(in)
		_, hasErr := errs["refresh_seconds"]
		if hasErr != tt.wantErr {
			t.Errorf("refresh %d: error = %v, want error %v (errors: %v)", tt.seconds, hasErr, tt.wantErr, errs)
		}
	}
}

func TestValidateTablePageSize(t *testing.T) {
	tests := []struct {
		size    int
		wantErr bool
	}{
		{size: -1, wantErr: true},
		{size: 0, wantErr: true},
		{size: MinTablePageSize - 1, wantErr: true},
		{size: MinTablePageSize},
		{size: 20},
		{size: MaxTablePageSize},
		{size: MaxTablePageSize + 1, wantErr: true},
	}
	for _, tt := range tests {
		in := validSettings()
		in.TablePageSize = tt.size
		_, errs := Validate(in)
		_, hasErr := errs["table_page_size"]
		if hasErr != tt.wantErr {
			t.Errorf("page size %d: error = %v, want error %v (errors: %v)", tt.size, hasErr, tt.wantErr, errs)
		}
	}
}

func TestValidateColours(t *testing.T) {
	tests := []struct {
		name    string
		colour  string
		want    string
		wantErr bool
	}{
		{name: "lowercase", colour: "#aabbcc", want: "#aabbcc"},
		{name: "uppercase is stored lowercase", colour: "#AABBCC", want: "#aabbcc"},
		{name: "mixed case", colour: "#AaBb0C", want: "#aabb0c"},
		{name: "surrounding spaces are trimmed", colour: "  #AABBCC ", want: "#aabbcc"},
		{name: "missing hash", colour: "aabbcc", wantErr: true},
		{name: "short form", colour: "#abc", wantErr: true},
		{name: "too long", colour: "#aabbccd", wantErr: true},
		{name: "not hex", colour: "#gghhii", wantErr: true},
		{name: "empty", colour: "", wantErr: true},
		{name: "named colour", colour: "red", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validSettings()
			in.ColourClock = tt.colour
			got, errs := Validate(in)
			_, hasErr := errs["colour_clock"]
			if hasErr != tt.wantErr {
				t.Fatalf("colour %q: error = %v, want error %v (errors: %v)", tt.colour, hasErr, tt.wantErr, errs)
			}
			if !tt.wantErr && got.ColourClock != tt.want {
				t.Errorf("ColourClock = %q, want %q", got.ColourClock, tt.want)
			}
		})
	}
}

func TestValidateReportsEachField(t *testing.T) {
	in := Settings{
		Title:            strings.Repeat("x", 81),
		RefreshSeconds:   0,
		ColourBackground: "nope",
		ColourLabel:      "#zzzzzz",
		ColourClock:      "#123",
		ColourUTC:        "",
		TablePageSize:    0,
	}
	_, errs := Validate(in)
	for _, field := range []string{"title", "refresh_seconds", "colour_background", "colour_label", "colour_clock", "colour_utc", "table_page_size"} {
		if _, ok := errs[field]; !ok {
			t.Errorf("missing error for %s: %v", field, errs)
		}
	}
}

func TestValidateAcceptsDefaults(t *testing.T) {
	if _, errs := Validate(validSettings()); len(errs) != 0 {
		t.Errorf("defaults rejected: %v", errs)
	}
}

// openTestDB returns a fresh database with the real migrations applied.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

func TestGetReturnsSeedDefaults(t *testing.T) {
	database := openTestDB(t)
	got, err := Get(context.Background(), database)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != validSettings() {
		t.Errorf("Get = %+v, want seed defaults %+v", got, validSettings())
	}
}

func TestUpdateStoresNormalisedValues(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	in := validSettings()
	in.Title = "  Ports  "
	in.RefreshSeconds = 10
	in.ColourBackground = "#FFFFFF"
	stored, err := Update(ctx, database, in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if stored.Title != "Ports" || stored.ColourBackground != "#ffffff" {
		t.Errorf("Update returned %+v, want trimmed title and lowercase colour", stored)
	}

	got, err := Get(ctx, database)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != stored {
		t.Errorf("Get = %+v, want %+v", got, stored)
	}
}

func TestUpdateRejectsInvalidAndLeavesRowUnchanged(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	in := validSettings()
	in.RefreshSeconds = 99
	_, err := Update(ctx, database, in)

	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("Update error = %v, want Errors", err)
	}
	if _, ok := errs["refresh_seconds"]; !ok {
		t.Errorf("Errors = %v, want refresh_seconds", errs)
	}

	got, err := Get(ctx, database)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != validSettings() {
		t.Errorf("row changed after rejected update: %+v", got)
	}
}
