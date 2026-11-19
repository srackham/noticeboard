package admintable

import "database/sql"

// All returns the administrable tables, in no particular order. Adding a table
// means adding it here and writing its adapter.
func All(db *sql.DB) []Table {
	return []Table{
		NewTimezones(db),
	}
}
