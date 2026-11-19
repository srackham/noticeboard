package zones

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"noticeboard/internal/db"
)

// openPagesDB returns a fresh migrated database with the seed clocks.
func openPagesDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

func TestListPage(t *testing.T) {
	sqlDB := openPagesDB(t)
	ctx := t.Context()

	first, err := ListPage(ctx, sqlDB, 1, 4, "")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if first.Number != 1 || first.Pages != 3 || first.Total != 9 {
		t.Errorf("page = %+v, want page 1 of 3, total 9", first)
	}
	if len(first.Zones) != 4 || first.Zones[0].Label != "UTC" {
		t.Errorf("first page zones = %v, want four rows starting with UTC", first.Zones)
	}

	last, err := ListPage(ctx, sqlDB, 3, 4, "")
	if err != nil {
		t.Fatalf("ListPage page 3: %v", err)
	}
	if len(last.Zones) != 1 || last.Zones[0].Label != "Ukraine" {
		t.Errorf("last page = %v, want the single Ukraine row", last.Zones)
	}

	// A page past the end is clamped, and the page shown is returned.
	over, err := ListPage(ctx, sqlDB, 99, 4, "")
	if err != nil {
		t.Fatalf("ListPage page 99: %v", err)
	}
	if over.Number != 3 || len(over.Zones) != 1 {
		t.Errorf("clamped page = %+v, want the last page", over)
	}

	// Disabled clocks are counted and listed.
	if err := Toggle(ctx, sqlDB, first.Zones[1].ID); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	p, err := ListPage(ctx, sqlDB, 1, 4, "")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if p.Total != 9 || len(p.Zones) != 4 || p.Zones[1].Enabled {
		t.Errorf("page with a disabled clock = %+v", p)
	}
}

func TestListPageEmpty(t *testing.T) {
	sqlDB := openPagesDB(t)
	if _, err := sqlDB.Exec(`DELETE FROM timezones`); err != nil {
		t.Fatalf("delete clocks: %v", err)
	}
	p, err := ListPage(t.Context(), sqlDB, 5, 20, "")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if p.Number != 1 || p.Pages != 1 || p.Total != 0 || len(p.Zones) != 0 {
		t.Errorf("empty table = %+v, want page 1 of 1 with no rows", p)
	}
}

func TestListPageFollowsSortOrder(t *testing.T) {
	sqlDB := openPagesDB(t)
	ctx := t.Context()

	var id int64
	if err := sqlDB.QueryRow(`SELECT id FROM timezones WHERE zone = 'UTC'`).Scan(&id); err != nil {
		t.Fatalf("find UTC: %v", err)
	}
	if err := Move(ctx, sqlDB, id, Down); err != nil {
		t.Fatalf("Move: %v", err)
	}
	p, err := ListPage(ctx, sqlDB, 1, 2, "")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if len(p.Zones) != 2 || p.Zones[0].Zone != "Pacific/Auckland" || p.Zones[1].Zone != "UTC" {
		t.Errorf("order = %v, want the moved clock second", p.Zones)
	}
}

func TestListPageSearch(t *testing.T) {
	sqlDB := openPagesDB(t)
	ctx := t.Context()

	// Matches the zone of India, China and the Philippines.
	p, err := ListPage(ctx, sqlDB, 1, 20, "Asia")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if p.Total != 3 || len(p.Zones) != 3 || p.Pages != 1 {
		t.Errorf("search Asia = %d rows, total %d, pages %d; want 3, 3, 1", len(p.Zones), p.Total, p.Pages)
	}
	if got := labelsOf(p.Zones); strings.Join(got, "|") != "India|China|Philippines" {
		t.Errorf("search Asia labels = %v", got)
	}

	// The match is case-insensitive for ASCII.
	lower, err := ListPage(ctx, sqlDB, 1, 20, "asia")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if lower.Total != p.Total {
		t.Errorf("case-insensitive search total = %d, want %d", lower.Total, p.Total)
	}

	// Rows can also match on the numeric columns.
	byID, err := ListPage(ctx, sqlDB, 1, 20, "UTC")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if byID.Total != 1 || byID.Zones[0].Label != "UTC" {
		t.Errorf("search UTC = %v, want the UTC clock only", byID.Zones)
	}

	// A blank search keeps every clock.
	blank, err := ListPage(ctx, sqlDB, 1, 20, "   ")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if blank.Total != 9 {
		t.Errorf("blank search total = %d, want 9", blank.Total)
	}

	// LIKE's wildcards are matched literally.
	for _, wildcard := range []string{"%", "_"} {
		none, err := ListPage(ctx, sqlDB, 1, 20, wildcard)
		if err != nil {
			t.Fatalf("ListPage %q: %v", wildcard, err)
		}
		if none.Total != 0 {
			t.Errorf("search %q total = %d, want 0 (no literal match)", wildcard, none.Total)
		}
	}

	// No match gives an empty page 1 of 1.
	none, err := ListPage(ctx, sqlDB, 3, 20, "Nowhere")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if none.Total != 0 || none.Number != 1 || none.Pages != 1 || len(none.Zones) != 0 {
		t.Errorf("no match = %+v, want page 1 of 1 with no rows", none)
	}
}

func TestListPageSearchPaginates(t *testing.T) {
	sqlDB := openPagesDB(t)
	ctx := t.Context()

	first, err := ListPage(ctx, sqlDB, 1, 2, "a")
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if first.Pages < 3 || first.Total < 3 {
		t.Errorf("search 'a' = %d pages of %d rows, want several", first.Pages, first.Total)
	}
	second, err := ListPage(ctx, sqlDB, 2, 2, "a")
	if err != nil {
		t.Fatalf("ListPage page 2: %v", err)
	}
	if second.Number != 2 || second.Total != first.Total {
		t.Errorf("page 2 = %+v, want page 2 of the same filtered total %d", second, first.Total)
	}
}
