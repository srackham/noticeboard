// Package zones is the repository for the clocks in the timezones table
// (DESIGN.md section 3.1).
package zones

import (
	"context"
	"database/sql"
	"fmt"
)

// Zone is one clock.
type Zone struct {
	ID      int64
	Label   string
	Zone    string // IANA zone ID, such as "Australia/Sydney"
	Enabled bool
	Sort    int
}

// ListEnabled returns the enabled clocks in display order.
func ListEnabled(ctx context.Context, db *sql.DB) ([]Zone, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, label, zone, enabled, sort
		FROM timezones WHERE enabled = 1 ORDER BY sort`)
	if err != nil {
		return nil, fmt.Errorf("list clocks: %w", err)
	}
	defer rows.Close()

	var zs []Zone
	for rows.Next() {
		var z Zone
		if err := rows.Scan(&z.ID, &z.Label, &z.Zone, &z.Enabled, &z.Sort); err != nil {
			return nil, fmt.Errorf("list clocks: %w", err)
		}
		zs = append(zs, z)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list clocks: %w", err)
	}
	return zs, nil
}
