// Package migrations embeds the SQL schema migrations, which internal/db
// applies in order.
package migrations

import "embed"

// FS holds the migration files, named NNNN_description.sql with NNNN the
// version number, starting at 0001 and with no gaps.
//
//go:embed *.sql
var FS embed.FS
