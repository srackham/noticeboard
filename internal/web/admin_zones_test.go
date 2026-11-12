package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"noticeboard/internal/auditlog"
)

// adminSession seeds the administrator and returns a session cookie for it.
func adminSession(t *testing.T, h http.Handler, sqlDB *sql.DB) *http.Cookie {
	t.Helper()
	seedAdmin(t, sqlDB)
	c := sessionCookie(login(h, adminEmail, adminPassword))
	if c == nil {
		t.Fatal("login did not set a session cookie")
	}
	return c
}

// adminRequest builds an HTMX request as the admin pages send it. A nil form sends no body.
func adminRequest(method, path string, form url.Values, c *http.Cookie) *http.Request {
	if form == nil {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("HX-Request", "true")
		return withCookie(req, c)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	return withCookie(req, c)
}

// clockID returns the ID of the seeded clock with the given zone.
func clockID(t *testing.T, sqlDB *sql.DB, zone string) string {
	t.Helper()
	var id int64
	if err := sqlDB.QueryRow(`SELECT id FROM timezones WHERE zone = ?`, zone).Scan(&id); err != nil {
		t.Fatalf("id of %s: %v", zone, err)
	}
	return strconv.FormatInt(id, 10)
}

// enabledCount returns how many clocks are enabled.
func enabledCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM timezones WHERE enabled = 1`).Scan(&n); err != nil {
		t.Fatalf("count enabled: %v", err)
	}
	return n
}

// zoneOrder returns the labels of all clocks in display order.
func zoneOrder(t *testing.T, sqlDB *sql.DB) []string {
	t.Helper()
	rows, err := sqlDB.Query(`SELECT label FROM timezones ORDER BY sort`)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestDashboardListsClocks(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin", nil), c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if n := strings.Count(body, `data-testid="zone-row"`); n != 9 {
		t.Errorf("rows = %d, want 9", n)
	}
	for _, want := range []string{
		`id="zones"`,
		`id="clock-form"`,
		"9 of 9 clocks enabled.",
		`hx-get="/admin/zones/new"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestNewClockForm(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin/zones/new", nil), c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `hx-post="/admin/zones"`) || !strings.Contains(body, "New clock") {
		t.Errorf("new clock form wrong:\n%s", body)
	}
}

func TestCreateClock(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	// Free a place on the full board first.
	serve(h, adminRequest(http.MethodPost, "/admin/zones/"+clockID(t, sqlDB, "Europe/Kyiv")+"/toggle", nil, c))

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones",
		url.Values{"label": {"Harbour"}, "zone": {"Asia/Tokyo"}}, c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<div id="zones" hx-swap-oob="true"`,
		`<div id="clock-form" hx-swap-oob="true"></div>`,
		"Harbour",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q:\n%s", want, body)
		}
	}
	if got := zoneOrder(t, sqlDB); got[len(got)-1] != "Harbour" {
		t.Errorf("new clock not at the end: %v", got)
	}
}

func TestCreateClockValidation(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones",
		url.Values{"label": {""}, "zone": {"Not/AZone"}}, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="zone-form"`,
		`hx-post="/admin/zones"`,
		"Label is required.",
		"Unknown time zone.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("422 response missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `hx-swap-oob`) {
		t.Error("validation failure should swap only the form region")
	}
}

func TestCreateClockRefusedWhenNineEnabled(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones",
		url.Values{"label": {"Harbour"}, "zone": {"Asia/Tokyo"}}, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Nine clocks are already enabled") {
		t.Errorf("limit message missing:\n%s", rec.Body.String())
	}
}

func TestEditClockForm(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "UTC")

	rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin/zones/"+id+"/edit", nil), c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Edit clock",
		`hx-post="/admin/zones/` + id + `"`,
		`value="UTC"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q", want)
		}
	}
}

func TestUnknownClockIsNotFound(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	requests := []*http.Request{
		withCookie(httptest.NewRequest(http.MethodGet, "/admin/zones/999/edit", nil), c),
		withCookie(httptest.NewRequest(http.MethodGet, "/admin/zones/abc/edit", nil), c),
		adminRequest(http.MethodPost, "/admin/zones/999", url.Values{"label": {"x"}, "zone": {"UTC"}}, c),
		adminRequest(http.MethodPost, "/admin/zones/999/toggle", nil, c),
		adminRequest(http.MethodPost, "/admin/zones/999/move?dir=up", nil, c),
		adminRequest(http.MethodDelete, "/admin/zones/999", nil, c),
	}
	for _, req := range requests {
		if rec := serve(h, req); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", req.Method, req.URL.Path, rec.Code)
		}
	}
}

func TestUpdateClock(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "UTC")

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones/"+id,
		url.Values{"label": {"Universal"}, "zone": {"UTC"}}, c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `hx-swap-oob="true"`) {
		t.Error("success should swap the list out of band")
	}
	if got := zoneOrder(t, sqlDB)[0]; got != "Universal" {
		t.Errorf("first label = %q, want Universal", got)
	}
}

func TestUpdateClockDuplicateIsValidationError(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones/"+clockID(t, sqlDB, "Asia/Kolkata"),
		url.Values{"label": {"UTC"}, "zone": {"Asia/Kolkata"}}, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "already used") {
		t.Errorf("duplicate message missing:\n%s", rec.Body.String())
	}
}

func TestToggleClock(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "Europe/Kyiv")

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones/"+id+"/toggle", nil, c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if enabledCount(t, sqlDB) != 8 {
		t.Errorf("enabled = %d after disabling, want 8", enabledCount(t, sqlDB))
	}
	if !strings.Contains(rec.Body.String(), "8 of 9 clocks enabled.") {
		t.Errorf("count not refreshed:\n%s", rec.Body.String())
	}
}

func TestToggleEnableRefusedWhenNineEnabled(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	kyiv := clockID(t, sqlDB, "Europe/Kyiv")
	serve(h, adminRequest(http.MethodPost, "/admin/zones/"+kyiv+"/toggle", nil, c))
	serve(h, adminRequest(http.MethodPost, "/admin/zones",
		url.Values{"label": {"Harbour"}, "zone": {"Asia/Tokyo"}}, c))

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones/"+kyiv+"/toggle", nil, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<div id="clock-form" hx-swap-oob="true">`) ||
		!strings.Contains(body, "Nine clocks are already enabled") {
		t.Errorf("error not shown in the form region:\n%s", body)
	}
	if strings.Contains(body, `id="zones"`) {
		t.Error("refused toggle should not replace the list")
	}
}

func TestMoveClock(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	india := clockID(t, sqlDB, "Asia/Kolkata")

	rec := serve(h, adminRequest(http.MethodPost, "/admin/zones/"+india+"/move?dir=up", nil, c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := zoneOrder(t, sqlDB); got[1] != "India" {
		t.Errorf("order = %v, want India second", got)
	}

	if rec := serve(h, adminRequest(http.MethodPost, "/admin/zones/"+india+"/move?dir=sideways", nil, c)); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid dir: status = %d, want 400", rec.Code)
	}
	if rec := serve(h, adminRequest(http.MethodPost, "/admin/zones/"+india+"/move", nil, c)); rec.Code != http.StatusBadRequest {
		t.Errorf("missing dir: status = %d, want 400", rec.Code)
	}
}

func TestDeleteClock(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	india := clockID(t, sqlDB, "Asia/Kolkata")

	rec := serve(h, adminRequest(http.MethodDelete, "/admin/zones/"+india, nil, c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Asia/Kolkata") {
		t.Error("deleted clock still in the list")
	}
	if n := len(zoneOrder(t, sqlDB)); n != 8 {
		t.Errorf("clocks = %d, want 8", n)
	}
}

func TestDeleteRequiresHTMX(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	req := withCookie(httptest.NewRequest(http.MethodDelete, "/admin/zones/"+clockID(t, sqlDB, "Asia/Kolkata"), nil), c)
	if rec := serve(h, req); rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestClockActionsNeedSession(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	id := clockID(t, sqlDB, "UTC")

	req := httptest.NewRequest(http.MethodPost, "/admin/zones/"+id+"/toggle", nil)
	req.Header.Set("HX-Request", "true")
	if rec := serve(h, req); rec.Code != http.StatusUnauthorized || rec.Header().Get("HX-Redirect") != "/admin/login" {
		t.Errorf("status %d, HX-Redirect %q; want 401 and /admin/login", rec.Code, rec.Header().Get("HX-Redirect"))
	}
}

func TestClockChangesAreAudited(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.log")
	audit, err := auditlog.Open(logPath)
	if err != nil {
		t.Fatalf("auditlog.Open: %v", err)
	}
	srv, sqlDB := newServer(t, audit)
	h := srv.Handler()
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "Asia/Kolkata")

	serve(h, adminRequest(http.MethodDelete, "/admin/zones/"+id, nil, c))

	lines := auditLines(t, logPath)
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "DELETE /admin/zones/"+id+" 200") {
		t.Errorf("audit lines = %q, want a DELETE line", lines)
	}
}
