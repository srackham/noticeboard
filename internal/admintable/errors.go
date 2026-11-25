package admintable

import (
	"errors"
	"sort"
	"strings"
)

// ErrNotFound means no record has the given ID.
var ErrNotFound = errors.New("record not found")

// FieldErrors maps a field name to a message for the user. It is returned as an
// error when a field is invalid.
type FieldErrors map[string]string

// Error joins the messages in field-name order.
func (e FieldErrors) Error() string {
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

// Refused is a save the table's rules rejects as a whole, such as adding a
// clock when the clock limit is reached.
type Refused struct {
	Message string
}

// Error returns the message for the user.
func (r Refused) Error() string { return r.Message }
