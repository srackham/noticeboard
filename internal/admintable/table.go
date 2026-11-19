// Package admintable defines the generic administration table layer. A table
// adapter implements Table over a repository, and the handlers in internal/web
// render every table through that contract. The package has no HTTP and no SQL:
// only the registry (registry.go) touches a database handle.
//
// Table-specific words, such as "clock", belong in an adapter and its tests.
// The handlers and templates must not use them.
package admintable

import (
	"context"
)

// Table is one administrable table. An adapter implements it over a repository.
// The grid shows the table's own columns, and the form edits the fields the
// adapter names.
type Table interface {
	// Slug is the URL segment and registry key, such as "timezones".
	Slug() string
	// Title is the page heading, such as "Time zones".
	Title() string
	// Columns are the table's column titles, in display order, including the
	// primary key. List returns one raw value per column.
	Columns() []string
	// Fields are the editable form fields in display order. They are a subset
	// of the columns: the primary key is not editable.
	Fields() []Field
	// Defaults returns the form values for a new record, such as the next sort
	// value. It may return nil for an empty form.
	Defaults(ctx context.Context) (Values, error)
	// List returns one page of rows. A non-empty search keeps only the
	// records whose values contain it, in any of the table's columns.
	List(ctx context.Context, pg Paging, search string) (Page, error)
	// Load returns the form values for the record with the given ID, or
	// ErrNotFound.
	Load(ctx context.Context, id int64) (Values, error)
	// Create adds a record. It returns FieldErrors or Refused when the
	// record's rules reject the input.
	Create(ctx context.Context, v Values) error
	// Update changes a record's fields, or returns ErrNotFound, FieldErrors
	// or Refused.
	Update(ctx context.Context, id int64, v Values) error
	// Delete removes a record, or returns ErrNotFound.
	Delete(ctx context.Context, id int64) error
}

// Paging is a page request. Page is 1-based. List returns the page actually
// shown, which may differ after clamping.
type Paging struct {
	Page int
	Size int
}

// Page is one page of a table.
type Page struct {
	Rows   []Row
	Number int // page shown, after clamping
	Pages  int // at least 1
	Total  int // rows in the whole table
	Offset int // index of the first row on this page, from 0
}

// Row is one record in the grid.
type Row struct {
	ID    int64
	Cells []string // one raw value per Table.Columns entry, in the same order
}

// Field is one form input.
type Field struct {
	Name      string
	Label     string
	MaxLength int                           // 0 for none
	Options   func(current string) []Option // set for select fields; nil for text
}

// Option is one entry in a select field.
type Option struct {
	Group    string // optgroup name, such as "Europe"
	Value    string
	Selected bool
}

// Values holds a record's form values, keyed by Field.Name, as typed. Keeping
// the typed strings lets a rejected form be shown again unchanged.
type Values map[string]string
