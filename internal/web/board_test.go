package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"noticeboard/internal/auditlog"
	"noticeboard/internal/db"
)

// fixedNow is 2024-06-01 14:05 UTC, so the expected times below are fixed.
var fixedNow = time.Date(2024, time.June, 1, 14, 5, 0, 0, time.UTC)

// newTestServer returns the handler and a fresh migrated database, without audit logging.
func newTestServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	srv, sqlDB := newServer(t, nil)
	return srv.Handler(), sqlDB
}

// newServer opens a fresh migrated database and returns a server that uses
// audit (which may be nil). The real templates and static files are read from
// the repository root.
func newServer(t *testing.T, audit *auditlog.Logger) (*Server, *sql.DB) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	srv, err := New(sqlDB, os.DirFS("../.."), audit)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.now = func() time.Time { return fixedNow }
	return srv, sqlDB
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// getBoard fetches the board fragment the way HTMX polls it.
func getBoard(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/clocks", nil)
	req.Header.Set("HX-Request", "true")
	h.ServeHTTP(rec, req)
	return rec
}

func exec(t *testing.T, sqlDB *sql.DB, query string) {
	t.Helper()
	if _, err := sqlDB.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// tileRE captures each tile's zone, UTC flag and displayed time, in order.
var tileRE = regexp.MustCompile(`data-zone="([^"]*)" data-utc="(true|false)">[\s\S]*?<time datetime="([^"]*)">`)

type tile struct{ zone, utc, time string }

func tiles(body string) []tile {
	var out []tile
	for _, m := range tileRE.FindAllStringSubmatch(body, -1) {
		out = append(out, tile{zone: m[1], utc: m[2], time: m[3]})
	}
	return out
}

func TestBoardShowsSeedClocksInOrder(t *testing.T) {
	h, _ := newTestServer(t)
	rec := getBoard(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	want := []tile{
		{"UTC", "true", "14:05"},
		{"Pacific/Auckland", "false", "02:05"},
		{"Asia/Kolkata", "false", "19:35"},
		{"Australia/Sydney", "false", "00:05"},
		{"Asia/Shanghai", "false", "22:05"},
		{"America/Vancouver", "false", "07:05"},
		{"Asia/Manila", "false", "22:05"},
		{"Europe/Moscow", "false", "17:05"},
		{"Europe/Kyiv", "false", "17:05"},
	}
	got := tiles(rec.Body.String())
	if len(got) != len(want) {
		t.Fatalf("got %d tiles, want %d:\n%s", len(got), len(want), rec.Body.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tile %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBoardWrapperAttributes(t *testing.T) {
	h, sqlDB := newTestServer(t)
	exec(t, sqlDB, `UPDATE settings SET refresh_seconds = 12`)
	body := getBoard(t, h).Body.String()

	for _, want := range []string{
		`id="board"`,
		`data-testid="board"`,
		`hx-get="/clocks"`,
		`hx-trigger="every 12s"`,
		`hx-swap="outerHTML"`,
		`--colour-background:#d9d9d9`,
		`--colour-label:#4169e1`,
		`--colour-clock:#228b22`,
		`--colour-utc:#b22222`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "<html") {
		t.Error("the poll must return the fragment, not the full page")
	}
}

func TestBoardTilesMarkup(t *testing.T) {
	h, _ := newTestServer(t)
	body := getBoard(t, h).Body.String()

	if n := strings.Count(body, `data-testid="clock"`); n != 9 {
		t.Errorf("clock tiles = %d, want 9", n)
	}
	if !strings.Contains(body, `class="tile utc" data-testid="clock" data-zone="UTC" data-utc="true"`) {
		t.Errorf("UTC tile should use the utc class:\n%s", body)
	}
	if !strings.Contains(body, `class="tile clock" data-testid="clock" data-zone="Asia/Kolkata"`) {
		t.Errorf("non-UTC tile should use the clock class:\n%s", body)
	}
	if !strings.Contains(body, `<div class="label">New Zealand</div>`) {
		t.Errorf("label missing:\n%s", body)
	}
}

func TestBoardHidesDisabledClocks(t *testing.T) {
	h, sqlDB := newTestServer(t)
	exec(t, sqlDB, `UPDATE timezones SET enabled = 0 WHERE zone = 'Asia/Kolkata'`)

	body := getBoard(t, h).Body.String()
	if strings.Contains(body, "Asia/Kolkata") {
		t.Error("disabled clock was rendered")
	}
	if n := len(tiles(body)); n != 8 {
		t.Errorf("tiles = %d, want 8", n)
	}
}

func TestBoardEmptyMessage(t *testing.T) {
	h, sqlDB := newTestServer(t)
	exec(t, sqlDB, `UPDATE timezones SET enabled = 0`)

	body := getBoard(t, h).Body.String()
	if !strings.Contains(body, "No clocks are enabled.") {
		t.Errorf("empty message missing:\n%s", body)
	}
	if n := len(tiles(body)); n != 0 {
		t.Errorf("tiles = %d, want 0", n)
	}
}

func TestBoardTitle(t *testing.T) {
	h, sqlDB := newTestServer(t)

	body := getBoard(t, h).Body.String()
	if !strings.Contains(body, `<h1 class="title" data-testid="title">World Clocks</h1>`) {
		t.Errorf("default title missing:\n%s", body)
	}

	exec(t, sqlDB, `UPDATE settings SET title = ''`)
	body = getBoard(t, h).Body.String()
	if strings.Contains(body, `data-testid="title"`) {
		t.Error("blank title should hide the header")
	}
}

func TestBoardEscapesLabelsAndZones(t *testing.T) {
	h, sqlDB := newTestServer(t)
	exec(t, sqlDB, `UPDATE timezones SET label = '<script>x</script>' WHERE zone = 'UTC'`)

	body := getBoard(t, h).Body.String()
	if strings.Contains(body, "<script>x</script>") {
		t.Error("label was not escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;x&lt;/script&gt;") {
		t.Errorf("escaped label missing:\n%s", body)
	}
}

func TestBoardInvalidZoneShowsPlaceholder(t *testing.T) {
	h, sqlDB := newTestServer(t)
	exec(t, sqlDB, `UPDATE timezones SET zone = 'Not/AZone' WHERE zone = 'Europe/Kyiv'`)

	got := tiles(getBoard(t, h).Body.String())
	if last := got[len(got)-1]; last.time != "--:--" {
		t.Errorf("invalid zone tile = %+v, want time --:--", last)
	}
}

func TestBoardDatabaseErrorReturns500(t *testing.T) {
	h, sqlDB := newTestServer(t)
	sqlDB.Close()

	if rec := getBoard(t, h); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestIndexIsFullPage(t *testing.T) {
	h, _ := newTestServer(t)
	rec := get(t, h, "/clocks")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{
		"<!DOCTYPE html>",
		`<link rel="stylesheet" href="/static/css/app.css">`,
		`<script src="/static/js/htmx.min.js" defer></script>`,
		`<script src="/static/js/app.js" defer></script>`,
		`id="board"`,
		`data-testid="stale"`,
		`<a href="/admin" class="admin-link" data-testid="admin-link">ADMIN</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Error("inline scripts are not allowed by the CSP")
	}
	if n := len(tiles(body)); n != 9 {
		t.Errorf("index embeds %d tiles, want 9", n)
	}
}

func TestIndexTitleUsesSettingsTitle(t *testing.T) {
	h, sqlDB := newTestServer(t)
	exec(t, sqlDB, `UPDATE settings SET title = 'Harbour'`)
	body := get(t, h, "/clocks").Body.String()
	if !strings.Contains(body, "<title>Harbour</title>") {
		t.Errorf("page title missing:\n%s", body)
	}
}

func TestStaticFiles(t *testing.T) {
	h, _ := newTestServer(t)
	for _, path := range []string{
		"/static/css/app.css",
		"/static/js/app.js",
		"/static/js/htmx.min.js",
		"/static/fonts/DSEG7Classic-Bold.woff2",
	} {
		if rec := get(t, h, path); rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", path, rec.Code)
		}
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	h, _ := newTestServer(t)
	if rec := get(t, h, "/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
