// Package settings reads, validates and updates the board settings, the single
// row of the settings table (DESIGN.md section 3.2).
package settings

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Limits for the title and refresh interval.
const (
	MaxTitleLength = 80
	MinRefresh     = 1
	MaxRefresh     = 60
)

// Settings is the board configuration. Colours are "#rrggbb".
type Settings struct {
	Title            string
	RefreshSeconds   int
	ColourBackground string
	ColourLabel      string
	ColourClock      string
	ColourUTC        string
}

// Field names used as keys in Errors. They match the form field names.
const (
	FieldTitle            = "title"
	FieldRefreshSeconds   = "refresh_seconds"
	FieldColourBackground = "colour_background"
	FieldColourLabel      = "colour_label"
	FieldColourClock      = "colour_clock"
	FieldColourUTC        = "colour_utc"
)

// Errors maps a field name to a message for the user. It is returned as an
// error when input is invalid.
type Errors map[string]string

// Error joins the messages in field-name order.
func (e Errors) Error() string {
	fields := make([]string, 0, len(e))
	for field := range e {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	msgs := make([]string, 0, len(e))
	for _, field := range fields {
		msgs = append(msgs, e[field])
	}
	return strings.Join(msgs, " ")
}

var colourPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate trims and normalises s and checks each field. The returned
// Settings holds the normalised values even when errors are reported. The
// Errors value is nil when s is valid.
func Validate(s Settings) (Settings, Errors) {
	var errs Errors
	fail := func(field, msg string) {
		if errs == nil {
			errs = Errors{}
		}
		errs[field] = msg
	}

	s.Title = strings.TrimSpace(s.Title)
	if utf8.RuneCountInString(s.Title) > MaxTitleLength {
		fail(FieldTitle, fmt.Sprintf("Title must be %d characters or fewer.", MaxTitleLength))
	}

	if s.RefreshSeconds < MinRefresh || s.RefreshSeconds > MaxRefresh {
		fail(FieldRefreshSeconds, fmt.Sprintf("Refresh must be a whole number of seconds from %d to %d.", MinRefresh, MaxRefresh))
	}

	colours := []struct {
		field string
		label string
		value *string
	}{
		{FieldColourBackground, "Background colour", &s.ColourBackground},
		{FieldColourLabel, "Label colour", &s.ColourLabel},
		{FieldColourClock, "Clock colour", &s.ColourClock},
		{FieldColourUTC, "UTC colour", &s.ColourUTC},
	}
	for _, c := range colours {
		v := strings.TrimSpace(*c.value)
		if !colourPattern.MatchString(v) {
			fail(c.field, c.label+" must be # followed by six hex digits, such as #1a2b3c.")
			continue
		}
		*c.value = strings.ToLower(v)
	}

	return s, errs
}

// Get returns the settings row.
func Get(ctx context.Context, db *sql.DB) (Settings, error) {
	var s Settings
	err := db.QueryRowContext(ctx, `
		SELECT title, refresh_seconds, colour_background, colour_label, colour_clock, colour_utc
		FROM settings WHERE id = 1`).Scan(
		&s.Title, &s.RefreshSeconds, &s.ColourBackground, &s.ColourLabel, &s.ColourClock, &s.ColourUTC,
	)
	if err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}
	return s, nil
}

// Update validates s and, if it is valid, stores it. On invalid input it
// returns Errors and leaves the stored row unchanged. On success it returns the
// stored, normalised settings.
func Update(ctx context.Context, db *sql.DB, s Settings) (Settings, error) {
	clean, errs := Validate(s)
	if len(errs) > 0 {
		return s, errs
	}
	_, err := db.ExecContext(ctx, `
		UPDATE settings SET
			title = ?, refresh_seconds = ?, colour_background = ?,
			colour_label = ?, colour_clock = ?, colour_utc = ?
		WHERE id = 1`,
		clean.Title, clean.RefreshSeconds, clean.ColourBackground,
		clean.ColourLabel, clean.ColourClock, clean.ColourUTC,
	)
	if err != nil {
		return s, fmt.Errorf("update settings: %w", err)
	}
	return clean, nil
}
