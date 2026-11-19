package zones

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"noticeboard/internal/paging"
)

// Page is one page of clocks, in display order.
type Page struct {
	Zones  []Zone
	Number int // page shown, after clamping
	Pages  int // at least 1
	Total  int // clocks on the page's filter, enabled or not
}

// ListPage returns one page of every clock, enabled or not, in display order.
// The count and the rows are read in one transaction, so they agree. A page
// outside the range is clamped to the nearest end, and the page shown is
// returned.
//
// A non-empty search keeps only the clocks that have a value containing it, in
// any column shown in the table (id, label, zone, enabled, sort). The match is
// case-insensitive for ASCII, which is what SQLite's LIKE provides. The total
// and the page count are then those of the filtered set.
func ListPage(ctx context.Context, db *sql.DB, page, size int, search string) (Page, error) {
	where, args := searchFilter(search)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Page{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM timezones`+where, args...).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count clocks: %w", err)
	}
	number, pages := paging.Clamp(page, total, size)
	if size < 1 {
		size = 1
	}
	offset := (number - 1) * size

	args = append(args, size, offset)
	zs, err := query(ctx, tx,
		`SELECT id, label, zone, enabled, sort FROM timezones`+where+` ORDER BY sort LIMIT ? OFFSET ?`,
		args...)
	if err != nil {
		return Page{}, err
	}
	return Page{Zones: zs, Number: number, Pages: pages, Total: total}, nil
}

// searchFilter returns the WHERE clause and its arguments for a search. It
// returns an empty clause when the search is blank. The search text is matched
// literally: LIKE's wildcards in the text are escaped.
func searchFilter(search string) (string, []any) {
	search = strings.TrimSpace(search)
	if search == "" {
		return "", nil
	}
	pattern := "%" + escapeLike(search) + "%"
	where := ` WHERE (CAST(id AS TEXT) LIKE ? ESCAPE '\'
		OR label LIKE ? ESCAPE '\'
		OR zone LIKE ? ESCAPE '\'
		OR CAST(enabled AS TEXT) LIKE ? ESCAPE '\'
		OR CAST(sort AS TEXT) LIKE ? ESCAPE '\')`
	return where, []any{pattern, pattern, pattern, pattern, pattern}
}

// likePatternEscaper turns LIKE's wildcards into literals.
var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, `_`, `\_`)

// escapeLike makes s a literal LIKE pattern.
func escapeLike(s string) string {
	return likeEscaper.Replace(s)
}
