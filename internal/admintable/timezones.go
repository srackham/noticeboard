package admintable

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"noticeboard/internal/zones"
)

// Timezones maps the clocks in the timezones table to the Table contract. The
// grid shows the table's columns as stored, and the form edits every column
// except the ID. The rules (validation, uniqueness, the nine-enabled limit and
// ordering) live in internal/zones.
type Timezones struct {
	db *sql.DB
}

// NewTimezones returns the timezones adapter for db.
func NewTimezones(db *sql.DB) *Timezones {
	return &Timezones{db: db}
}

// Slug is the URL segment and registry key.
func (t *Timezones) Slug() string { return "timezones" }

// Title is the page heading.
func (t *Timezones) Title() string { return "Time zones" }

// Columns are the table's columns, in schema order.
func (t *Timezones) Columns() []string {
	return []string{"id", "label", "zone", "enabled", "sort"}
}

// Fields are the editable columns: every column except the ID.
func (t *Timezones) Fields() []Field {
	return []Field{
		{Name: "label", Label: "Label", MaxLength: zones.MaxLabelLength},
		{
			Name:  "zone",
			Label: "Time zone",
			Options: func(current string) []Option {
				return GroupByRegion(zones.Names(), current)
			},
		},
		{Name: "enabled", Label: "Enabled (0 or 1)"},
		{Name: "sort", Label: "Sort"},
	}
}

// Defaults returns a new record's values: enabled, and sorted after the last
// clock, as the dashboard does for a new clock.
func (t *Timezones) Defaults(ctx context.Context) (Values, error) {
	next, err := zones.NextSort(ctx, t.db)
	if err != nil {
		return nil, err
	}
	return Values{"enabled": "1", "sort": strconv.Itoa(next)}, nil
}

// List returns one page of clocks with their raw column values, in display
// order. A search keeps only the clocks with a value containing it.
func (t *Timezones) List(ctx context.Context, pg Paging, search string) (Page, error) {
	p, err := zones.ListPage(ctx, t.db, pg.Page, pg.Size, search)
	if err != nil {
		return Page{}, err
	}
	size := pg.Size
	if size < 1 {
		size = 1
	}
	offset := (p.Number - 1) * size

	rows := make([]Row, len(p.Zones))
	for i, z := range p.Zones {
		rows[i] = Row{
			ID:    z.ID,
			Cells: []string{strconv.FormatInt(z.ID, 10), z.Label, z.Zone, enabledValue(z.Enabled), strconv.Itoa(z.Sort)},
		}
	}
	return Page{Rows: rows, Number: p.Number, Pages: p.Pages, Total: p.Total, Offset: offset}, nil
}

// Load returns the form values for a clock.
func (t *Timezones) Load(ctx context.Context, id int64) (Values, error) {
	z, err := zones.Get(ctx, t.db, id)
	if err != nil {
		return nil, mapError(err)
	}
	return Values{
		"label":   z.Label,
		"zone":    z.Zone,
		"enabled": enabledValue(z.Enabled),
		"sort":    strconv.Itoa(z.Sort),
	}, nil
}

// Create adds a clock with the values from the form.
func (t *Timezones) Create(ctx context.Context, v Values) error {
	values, err := parseValues(v)
	if err != nil {
		return err
	}
	_, err = zones.CreateWith(ctx, t.db, values)
	return mapError(err)
}

// Update changes every editable value of a clock.
func (t *Timezones) Update(ctx context.Context, id int64, v Values) error {
	values, err := parseValues(v)
	if err != nil {
		return err
	}
	_, err = zones.UpdateWith(ctx, t.db, id, values)
	return mapError(err)
}

// Delete removes a clock.
func (t *Timezones) Delete(ctx context.Context, id int64) error {
	return mapError(zones.Delete(ctx, t.db, id))
}

// parseValues checks the enabled and sort form values and builds the
// repository input. The label and zone are checked by the repository.
func parseValues(v Values) (zones.Values, error) {
	var out zones.Values
	out.Label = v["label"]
	out.Zone = v["zone"]

	errs := FieldErrors{}
	switch strings.TrimSpace(v["enabled"]) {
	case "1":
		out.Enabled = true
	case "0":
		out.Enabled = false
	default:
		errs["enabled"] = "Enabled must be 0 or 1."
	}

	sortText := strings.TrimSpace(v["sort"])
	sort, err := strconv.Atoi(sortText)
	if err != nil {
		errs["sort"] = "Sort must be a whole number."
	} else {
		out.Sort = sort
	}

	if len(errs) > 0 {
		return zones.Values{}, errs
	}
	return out, nil
}

// enabledValue is the stored 0 or 1 for a flag.
func enabledValue(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// mapError converts a repository error to the contract's error types, so the
// generic handlers never see a zones error.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var fieldErrs zones.Errors
	switch {
	case errors.As(err, &fieldErrs):
		out := make(FieldErrors, len(fieldErrs))
		for field, msg := range fieldErrs {
			out[field] = msg
		}
		return out
	case errors.Is(err, zones.ErrTooManyClocks):
		return Refused{Message: err.Error()}
	case errors.Is(err, zones.ErrNotFound):
		return ErrNotFound
	}
	return fmt.Errorf("timezones table: %w", err)
}
