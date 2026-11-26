// Package settings reads, validates and updates the clock display settings, the single
// row of the settings table (DESIGN.md section 3.2).
package settings

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits for the title, refresh interval, table page size and login timeout.
const (
	MaxTitleLength   = 80
	MinRefresh       = 1
	MaxRefresh       = 60
	MinTablePageSize = 5
	MaxTablePageSize = 100
	MinLoginTimeout  = 0.5  // minutes
	MaxLoginTimeout  = 1440 // minutes, one day
)

// Settings is the clock display and administration configuration. Colours are "#rrggbb".
type Settings struct {
	Title            string
	RefreshSeconds   int
	ColourBackground string
	ColourLabel      string
	ColourClock      string
	ColourUTC        string
	TablePageSize    int     // rows per page in the administration table pages
	LoginTimeout     float64 // minutes without user activity before the admin pages sign out
}

// LoginTimeoutSeconds returns the login timeout in whole seconds, as the admin
// pages need it for their data-idle-timeout attribute.
func (s Settings) LoginTimeoutSeconds() int {
	return int(math.Round(s.LoginTimeout * 60))
}

// Field names used as keys in Errors. They match the form field names.
const (
	FieldTitle            = "title"
	FieldRefreshSeconds   = "refresh_seconds"
	FieldColourBackground = "colour_background"
	FieldColourLabel      = "colour_label"
	FieldColourClock      = "colour_clock"
	FieldColourUTC        = "colour_utc"
	FieldTablePageSize    = "table_page_size"
	FieldLoginTimeout     = "login_timeout"
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
		fail(FieldRefreshSeconds, refreshMessage())
	}

	if s.TablePageSize < MinTablePageSize || s.TablePageSize > MaxTablePageSize {
		fail(FieldTablePageSize, tablePageSizeMessage())
	}

	if math.IsNaN(s.LoginTimeout) || math.IsInf(s.LoginTimeout, 0) ||
		s.LoginTimeout < MinLoginTimeout || s.LoginTimeout > MaxLoginTimeout {
		fail(FieldLoginTimeout, loginTimeoutMessage())
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
		SELECT title, refresh_seconds, colour_background, colour_label, colour_clock, colour_utc,
		       table_page_size, login_timeout
		FROM settings WHERE id = 1`).Scan(
		&s.Title, &s.RefreshSeconds, &s.ColourBackground, &s.ColourLabel, &s.ColourClock, &s.ColourUTC,
		&s.TablePageSize, &s.LoginTimeout,
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
			colour_label = ?, colour_clock = ?, colour_utc = ?, table_page_size = ?,
			login_timeout = ?
		WHERE id = 1`,
		clean.Title, clean.RefreshSeconds, clean.ColourBackground,
		clean.ColourLabel, clean.ColourClock, clean.ColourUTC, clean.TablePageSize,
		clean.LoginTimeout,
	)
	if err != nil {
		return s, fmt.Errorf("update settings: %w", err)
	}
	return clean, nil
}

// Input holds the settings as typed in the form. Every value is a string, so
// that a rejected submission can be shown again exactly as it was typed.
type Input struct {
	Title            string
	RefreshSeconds   string
	ColourBackground string
	ColourLabel      string
	ColourClock      string
	ColourUTC        string
	TablePageSize    string
	LoginTimeout     string
}

// ParseInput parses and validates a form submission. It returns the normalised
// Settings and, when any field is wrong, Errors. A refresh value or page size
// that is not a whole number, or a login timeout that is not a number, is
// reported as an error for that field.
func ParseInput(in Input) (Settings, Errors) {
	s := Settings{
		Title:            in.Title,
		ColourBackground: in.ColourBackground,
		ColourLabel:      in.ColourLabel,
		ColourClock:      in.ColourClock,
		ColourUTC:        in.ColourUTC,
	}
	var errs Errors
	fail := func(field, msg string) {
		if errs == nil {
			errs = Errors{}
		}
		errs[field] = msg
	}

	if n, err := strconv.Atoi(strings.TrimSpace(in.RefreshSeconds)); err == nil {
		s.RefreshSeconds = n
	} else {
		fail(FieldRefreshSeconds, refreshMessage())
	}
	if n, err := strconv.Atoi(strings.TrimSpace(in.TablePageSize)); err == nil {
		s.TablePageSize = n
	} else {
		fail(FieldTablePageSize, tablePageSizeMessage())
	}
	if n, err := strconv.ParseFloat(strings.TrimSpace(in.LoginTimeout), 64); err == nil {
		s.LoginTimeout = n
	} else {
		fail(FieldLoginTimeout, loginTimeoutMessage())
	}

	clean, verrs := Validate(s)
	for field, msg := range verrs {
		if errs == nil {
			errs = Errors{}
		}
		if _, dup := errs[field]; !dup {
			errs[field] = msg
		}
	}
	return clean, errs
}

// refreshMessage is the message for an invalid refresh interval.
func refreshMessage() string {
	return fmt.Sprintf("Refresh must be a whole number of seconds from %d to %d.", MinRefresh, MaxRefresh)
}

// tablePageSizeMessage is the message for an invalid table page size.
func tablePageSizeMessage() string {
	return fmt.Sprintf("Rows per page must be a whole number from %d to %d.", MinTablePageSize, MaxTablePageSize)
}

// loginTimeoutMessage is the message for an invalid login timeout.
func loginTimeoutMessage() string {
	return fmt.Sprintf("Login timeout must be a number of minutes from %v to %v.", MinLoginTimeout, MaxLoginTimeout)
}
