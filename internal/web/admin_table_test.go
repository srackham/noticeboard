package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"noticeboard/internal/admintable"
	"noticeboard/internal/auditlog"
)

// The URL prefix of the table page used by the tests.
const timezonesPath = "/admin/tables/timezones"

// tableBody fetches a table page as the admin browser would.
func tableBody(t *testing.T, h http.Handler, c *http.Cookie, path string) string {
	t.Helper()
	req := withCookie(httptest.NewRequest(http.MethodGet, path, nil), c)
	req.Header.Set("HX-Request", "true")
	rec := serve(h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200:\n%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// tableFullPage fetches a table page as a normal browser request.
func tableFullPage(t *testing.T, h http.Handler, c *http.Cookie, path string) string {
	t.Helper()
	rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, path, nil), c))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200:\n%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// setPageSize stores the rows per page through the dashboard form.
func setPageSize(t *testing.T, h http.Handler, c *http.Cookie, size string) {
	t.Helper()
	form := validSettingsForm()
	form.Set("table_page_size", size)
	if rec := serve(h, adminRequest(http.MethodPost, "/admin/settings", form, c)); rec.Code != http.StatusOK {
		t.Fatalf("set page size: status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
}

func TestTablePageShowsRawColumns(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	body := tableFullPage(t, h, c, timezonesPath)
	for _, want := range []string{
		"<!DOCTYPE html>",
		"<th>id</th>",
		"<th>label</th>",
		"<th>zone</th>",
		"<th>enabled</th>",
		"<th>sort</th>",
		"Showing 1–9 of 9 records.",
		"Page 1 of 1",
		`hx-get="` + timezonesPath + `/new"`,
		`href="/admin"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("table page missing %q", want)
		}
	}
	if n := strings.Count(body, `data-testid="table-row"`); n != 9 {
		t.Errorf("rows = %d, want 9", n)
	}
	// The seed's first row is shown with its raw values.
	row := `<td>1</td><td>UTC</td><td>UTC</td><td>1</td><td>1</td>`
	if !strings.Contains(body, row) {
		t.Errorf("raw UTC row %q missing:\n%s", row, body)
	}
	// Only Edit and Delete are offered per row.
	if strings.Contains(body, ">Up<") || strings.Contains(body, ">Disable<") || strings.Contains(body, ">Enable<") {
		t.Error("row actions should be Edit and Delete only")
	}
	if !strings.Contains(body, `hx-confirm="Delete record 1?"`) {
		t.Error("Delete should confirm by record ID")
	}
}

func TestTablePageFragmentAndVary(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	req := withCookie(httptest.NewRequest(http.MethodGet, timezonesPath, nil), c)
	req.Header.Set("HX-Request", "true")
	rec := serve(h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Vary"); got != "HX-Request" {
		t.Errorf("Vary = %q, want HX-Request", got)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("fragment should not be a full page")
	}
	if !strings.Contains(body, `id="table-grid"`) {
		t.Error("fragment should contain the grid")
	}
}

func TestTablePageSizeComesFromSettings(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	setPageSize(t, h, c, "5")
	body := tableBody(t, h, c, timezonesPath)
	if n := strings.Count(body, `data-testid="table-row"`); n != 5 {
		t.Errorf("rows = %d, want 5 (the page size setting)", n)
	}
	if !strings.Contains(body, "Showing 1–5 of 9 records.") || !strings.Contains(body, "Page 1 of 2") {
		t.Errorf("count or pager wrong:\n%s", body)
	}

	second := tableBody(t, h, c, timezonesPath+"?page=2")
	if n := strings.Count(second, `data-testid="table-row"`); n != 4 {
		t.Errorf("page 2 rows = %d, want 4", n)
	}
	if !strings.Contains(second, "Showing 6–9 of 9 records.") || !strings.Contains(second, "Page 2 of 2") {
		t.Errorf("page 2 count or pager wrong:\n%s", second)
	}

	// A new size applies on the next request.
	setPageSize(t, h, c, "10")
	if n := strings.Count(tableBody(t, h, c, timezonesPath), `data-testid="table-row"`); n != 9 {
		t.Errorf("rows after size 10 = %d, want 9", n)
	}
}

func TestTablePageParsing(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	setPageSize(t, h, c, "5")

	for _, bad := range []string{"0", "-1", "abc", "1.5"} {
		rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, timezonesPath+"?page="+bad, nil), c))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("page=%s: status = %d, want 400", bad, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "page must be a positive whole number") {
			t.Errorf("page=%s: message missing:\n%s", bad, rec.Body.String())
		}
	}

	// A page past the end shows the last page.
	body := tableBody(t, h, c, timezonesPath+"?page=99")
	if !strings.Contains(body, "Page 2 of 2") || !strings.Contains(body, "Showing 6–9 of 9 records.") {
		t.Errorf("clamped page wrong:\n%s", body)
	}
}

func TestTablePageEmpty(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	exec(t, sqlDB, `DELETE FROM timezones`)

	body := tableBody(t, h, c, timezonesPath)
	if !strings.Contains(body, "No records yet.") {
		t.Errorf("empty message missing:\n%s", body)
	}
	if strings.Contains(body, `data-testid="pager"`) {
		t.Error("empty table should show no pager")
	}
	if !strings.Contains(tableFullPage(t, h, c, timezonesPath), `hx-get="`+timezonesPath+`/new"`) {
		t.Error("Add record should still be offered")
	}
}

func TestTableNewFormDefaults(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	body := tableBody(t, h, c, timezonesPath+"/new")
	for _, want := range []string{
		`hx-post="` + timezonesPath + `"`,
		`name="label"`,
		`name="zone"`,
		`name="enabled" type="text" value="1"`,
		`name="sort" type="text" value="10"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("new form missing %q:\n%s", want, body)
		}
	}
}

func TestTableCreate(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	form := url.Values{
		"label":   {"Honolulu"},
		"zone":    {"Pacific/Honolulu"},
		"enabled": {"0"},
		"sort":    {"42"},
	}
	rec := serve(h, adminRequest(http.MethodPost, timezonesPath, form, c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `hx-swap-oob="true"`) || !strings.Contains(body, `id="table-grid"`) {
		t.Errorf("response should refresh the grid out of band:\n%s", body)
	}
	if !strings.Contains(body, "<td>42</td>") || !strings.Contains(body, "Honolulu") {
		t.Errorf("new raw record missing from the refreshed grid:\n%s", body)
	}

	var enabled, sort int
	if err := sqlDB.QueryRow(`SELECT enabled, sort FROM timezones WHERE label = 'Honolulu'`).Scan(&enabled, &sort); err != nil {
		t.Fatalf("read created clock: %v", err)
	}
	if enabled != 0 || sort != 42 {
		t.Errorf("stored = enabled %d, sort %d; want 0, 42", enabled, sort)
	}
}

func TestTableCreateShowsLastPage(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	setPageSize(t, h, c, "5")

	form := url.Values{"label": {"Honolulu"}, "zone": {"Pacific/Honolulu"}, "enabled": {"0"}, "sort": {"42"}}
	body := serve(h, adminRequest(http.MethodPost, timezonesPath, form, c)).Body.String()
	if !strings.Contains(body, "Page 2 of 2") || !strings.Contains(body, "Honolulu") {
		t.Errorf("create should show the last page with the new record:\n%s", body)
	}
}

func TestTableCreateValidation(t *testing.T) {
	tests := []struct {
		name     string
		form     url.Values
		wantText string
	}{
		{
			name:     "duplicate label",
			form:     url.Values{"label": {"UTC"}, "zone": {"Europe/London"}, "enabled": {"0"}, "sort": {"42"}},
			wantText: "That label is already used by another clock.",
		},
		{
			name:     "unknown zone",
			form:     url.Values{"label": {"Mars"}, "zone": {"Mars/Olympus"}, "enabled": {"0"}, "sort": {"42"}},
			wantText: "Unknown time zone.",
		},
		{
			name:     "clashing sort",
			form:     url.Values{"label": {"Honolulu"}, "zone": {"Pacific/Honolulu"}, "enabled": {"0"}, "sort": {"1"}},
			wantText: "That sort value is already used by another clock.",
		},
		{
			name:     "enabled not 0 or 1",
			form:     url.Values{"label": {"Honolulu"}, "zone": {"Pacific/Honolulu"}, "enabled": {"yes"}, "sort": {"42"}},
			wantText: "Enabled must be 0 or 1.",
		},
		{
			name:     "sort not a number",
			form:     url.Values{"label": {"Honolulu"}, "zone": {"Pacific/Honolulu"}, "enabled": {"0"}, "sort": {"soon"}},
			wantText: "Sort must be a whole number.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, sqlDB := newTestServer(t)
			c := adminSession(t, h, sqlDB)

			rec := serve(h, adminRequest(http.MethodPost, timezonesPath, tt.form, c))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.wantText) {
				t.Errorf("message %q missing:\n%s", tt.wantText, body)
			}
			// The typed values come back unchanged.
			if !strings.Contains(body, `value="`+tt.form.Get("sort")+`"`) {
				t.Errorf("typed sort not kept:\n%s", body)
			}
		})
	}
}

func TestTableCreateRefusedWhenNineEnabled(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	form := url.Values{"label": {"Honolulu"}, "zone": {"Pacific/Honolulu"}, "enabled": {"1"}, "sort": {"42"}}
	rec := serve(h, adminRequest(http.MethodPost, timezonesPath, form, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Nine clocks are already enabled.") {
		t.Errorf("refusal message missing:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="table-form-error"`) {
		t.Error("refusal should be a form-level message")
	}
}

func TestTableEditFormShowsAllEditableFields(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "Australia/Sydney")

	body := tableBody(t, h, c, timezonesPath+"/"+id+"/edit?page=1")
	for _, want := range []string{
		`hx-post="` + timezonesPath + `/` + id + `?page=1"`,
		`value="Melbourne/Sydney"`,
		`<option value="Australia/Sydney" selected>Australia/Sydney</option>`,
		`name="enabled" type="text" value="1"`,
		`name="sort" type="text" value="4"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit form missing %q:\n%s", want, body)
		}
	}
}

func TestTableUpdate(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "Asia/Kolkata")

	// Free a place so the record can stay enabled while sort changes.
	exec(t, sqlDB, `UPDATE timezones SET enabled = 0 WHERE zone = 'Europe/Moscow'`)
	form := url.Values{"label": {"Delhi"}, "zone": {"Asia/Kolkata"}, "enabled": {"1"}, "sort": {"77"}}
	rec := serve(h, adminRequest(http.MethodPost, timezonesPath+"/"+id+"?page=1", form, c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Delhi") {
		t.Errorf("updated label missing from the grid:\n%s", rec.Body.String())
	}

	var label string
	var sort int
	if err := sqlDB.QueryRow(`SELECT label, sort FROM timezones WHERE id = ?`, id).Scan(&label, &sort); err != nil {
		t.Fatalf("read clock: %v", err)
	}
	if label != "Delhi" || sort != 77 {
		t.Errorf("stored = %q, sort %d; want Delhi, 77", label, sort)
	}
}

func TestTableUpdateCanDisableARecord(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "Europe/Moscow")

	form := url.Values{"label": {"Russia"}, "zone": {"Europe/Moscow"}, "enabled": {"0"}, "sort": {"8"}}
	if rec := serve(h, adminRequest(http.MethodPost, timezonesPath+"/"+id+"?page=1", form, c)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var enabled int
	sqlDB.QueryRow(`SELECT enabled FROM timezones WHERE id = ?`, id).Scan(&enabled)
	if enabled != 0 {
		t.Errorf("enabled = %d after disabling, want 0", enabled)
	}
}

func TestTableUpdateValidation(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "Asia/Kolkata")

	form := url.Values{"label": {"Delhi"}, "zone": {"Asia/Kolkata"}, "enabled": {"1"}, "sort": {"2"}}
	rec := serve(h, adminRequest(http.MethodPost, timezonesPath+"/"+id+"?page=1", form, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "That sort value is already used by another clock.") {
		t.Errorf("sort message missing:\n%s", body)
	}
	if !strings.Contains(body, `hx-post="`+timezonesPath+`/`+id+`?page=1"`) {
		t.Errorf("form should keep its action and page:\n%s", body)
	}
}

func TestTableUpdateRefusedWhenNineEnabled(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	// Disable one clock, then add an enabled one: nine are enabled and the
	// disabled clock cannot be turned on.
	utc := clockID(t, sqlDB, "UTC")
	form := url.Values{"label": {"UTC"}, "zone": {"UTC"}, "enabled": {"0"}, "sort": {"1"}}
	if rec := serve(h, adminRequest(http.MethodPost, timezonesPath+"/"+utc+"?page=1", form, c)); rec.Code != http.StatusOK {
		t.Fatalf("disable UTC: status = %d", rec.Code)
	}
	create := url.Values{"label": {"Honolulu"}, "zone": {"Pacific/Honolulu"}, "enabled": {"1"}, "sort": {"42"}}
	if rec := serve(h, adminRequest(http.MethodPost, timezonesPath, create, c)); rec.Code != http.StatusOK {
		t.Fatalf("create enabled clock: status = %d", rec.Code)
	}

	enable := url.Values{"label": {"UTC"}, "zone": {"UTC"}, "enabled": {"1"}, "sort": {"1"}}
	rec := serve(h, adminRequest(http.MethodPost, timezonesPath+"/"+utc+"?page=1", enable, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Nine clocks are already enabled.") {
		t.Errorf("refusal message missing:\n%s", rec.Body.String())
	}
}

func TestTableDelete(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	id := clockID(t, sqlDB, "Europe/Moscow")

	rec := serve(h, adminRequest(http.MethodDelete, timezonesPath+"/"+id+"?page=1", nil, c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	var n int
	sqlDB.QueryRow(`SELECT COUNT(*) FROM timezones WHERE id = ?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("record still present after delete")
	}
}

func TestTableDeleteClampsThePage(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	setPageSize(t, h, c, "5")
	for _, zone := range []string{"America/Vancouver", "Asia/Manila", "Europe/Moscow", "Europe/Kyiv"} {
		id := clockID(t, sqlDB, zone)
		rec := serve(h, adminRequest(http.MethodDelete, timezonesPath+"/"+id+"?page=2", nil, c))
		if rec.Code != http.StatusOK {
			t.Fatalf("delete %s: status = %d, want 200", zone, rec.Code)
		}
	}
	body := tableBody(t, h, c, timezonesPath+"?page=2")
	if !strings.Contains(body, "Page 1 of 1") || !strings.Contains(body, "Showing 1–5 of 5 records.") {
		t.Errorf("page not clamped after deleting the last page:\n%s", body)
	}
}

func TestTableUnknownRecord(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	missing := timezonesPath + "/999999"

	form := url.Values{"label": {"X"}, "zone": {"UTC"}, "enabled": {"1"}, "sort": {"99"}}
	for _, req := range []*http.Request{
		adminRequest(http.MethodPost, missing+"?page=1", form, c),
		adminRequest(http.MethodDelete, missing+"?page=1", nil, c),
	} {
		rec := serve(h, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", req.Method, rec.Code)
			continue
		}
		body := rec.Body.String()
		if !strings.Contains(body, "That record no longer exists.") {
			t.Errorf("%s: message missing:\n%s", req.Method, body)
		}
		if !strings.Contains(body, `id="table-grid"`) {
			t.Errorf("%s: the grid should be refreshed:\n%s", req.Method, body)
		}
	}

	rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, missing+"/edit", nil), c))
	if rec.Code != http.StatusNotFound {
		t.Errorf("edit status = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "That record no longer exists.") {
		t.Error("the edit form 404 should be plain text, not a fragment")
	}
}

func TestTableUnknownSlug(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	for _, path := range []string{"/admin/tables/nosuchtable", "/admin/tables/nosuchtable/new"} {
		rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, path, nil), c))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestTableNeedsSessionAndHTMX(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	req := httptest.NewRequest(http.MethodGet, timezonesPath, nil)
	req.Header.Set("HX-Request", "true")
	if rec := serve(h, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("GET without session: status = %d, want 401", rec.Code)
	}
	if rec := serve(h, httptest.NewRequest(http.MethodGet, timezonesPath, nil)); rec.Code != http.StatusSeeOther {
		t.Errorf("plain GET without session: status = %d, want 303", rec.Code)
	}

	c := sessionCookie(login(h, adminUsername, adminPassword))
	if c == nil {
		t.Fatal("login did not set a cookie")
	}
	form := url.Values{"label": {"X"}, "zone": {"UTC"}, "enabled": {"1"}, "sort": {"99"}}
	for _, req := range []*http.Request{
		withCookie(httptest.NewRequest(http.MethodPost, timezonesPath, strings.NewReader(form.Encode())), c),
		withCookie(httptest.NewRequest(http.MethodDelete, timezonesPath+"/1", nil), c),
		withCookie(httptest.NewRequest(http.MethodPost, timezonesPath+"/1", strings.NewReader(form.Encode())), c),
	} {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if rec := serve(h, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without HX-Request: status = %d, want 403", req.Method, req.URL.Path, rec.Code)
		}
	}
}

func TestTableChangesAreAudited(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.log")
	audit, err := auditlog.Open(logPath)
	if err != nil {
		t.Fatalf("auditlog.Open: %v", err)
	}
	srv, sqlDB := newServer(t, audit)
	h := srv.Handler()
	c := adminSession(t, h, sqlDB)

	form := url.Values{"label": {"Honolulu"}, "zone": {"Pacific/Honolulu"}, "enabled": {"0"}, "sort": {"42"}}
	serve(h, adminRequest(http.MethodPost, timezonesPath, form, c))
	id := clockID(t, sqlDB, "Asia/Kolkata")
	update := url.Values{"label": {"Delhi"}, "zone": {"Asia/Kolkata"}, "enabled": {"1"}, "sort": {"3"}}
	serve(h, adminRequest(http.MethodPost, timezonesPath+"/"+id+"?page=1", update, c))
	serve(h, adminRequest(http.MethodDelete, timezonesPath+"/"+id+"?page=1", nil, c))

	var lines []string
	for _, line := range auditLines(t, logPath) {
		if strings.Contains(line, "/admin/tables/") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("audit lines = %d, want 3:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for i, want := range []string{
		"POST " + timezonesPath + " 200",
		"POST " + timezonesPath + "/" + id + " 200",
		"DELETE " + timezonesPath + "/" + id + " 200",
	} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want it to contain %q", i, lines[i], want)
		}
	}
}

// notebookTable is a test-only second table, proving the handlers and templates
// are generic. It is not registered in admintable.All.
type notebookTable struct{ entries []string }

func (n *notebookTable) Slug() string  { return "notebook" }
func (n *notebookTable) Title() string { return "Notebook" }
func (n *notebookTable) Columns() []string {
	return []string{"id", "text"}
}
func (n *notebookTable) Fields() []admintable.Field {
	return []admintable.Field{{Name: "text", Label: "Text", MaxLength: 50}}
}
func (n *notebookTable) Defaults(ctx context.Context) (admintable.Values, error) {
	return admintable.Values{"text": "note"}, nil
}
func (n *notebookTable) List(ctx context.Context, pg admintable.Paging, search string) (admintable.Page, error) {
	rows := make([]admintable.Row, len(n.entries))
	for i, e := range n.entries {
		rows[i] = admintable.Row{ID: int64(i + 1), Cells: []string{strconv.Itoa(i + 1), e}}
	}
	return admintable.Page{Rows: rows, Number: 1, Pages: 1, Total: len(rows)}, nil
}
func (n *notebookTable) Load(ctx context.Context, id int64) (admintable.Values, error) {
	if id < 1 || id > int64(len(n.entries)) {
		return nil, admintable.ErrNotFound
	}
	return admintable.Values{"text": n.entries[id-1]}, nil
}
func (n *notebookTable) Create(ctx context.Context, v admintable.Values) error {
	if v["text"] == "" {
		return admintable.FieldErrors{"text": "Text is required."}
	}
	n.entries = append(n.entries, v["text"])
	return nil
}
func (n *notebookTable) Update(ctx context.Context, id int64, v admintable.Values) error {
	if id < 1 || id > int64(len(n.entries)) {
		return admintable.ErrNotFound
	}
	n.entries[id-1] = v["text"]
	return nil
}
func (n *notebookTable) Delete(ctx context.Context, id int64) error { return nil }

func TestTableLayerIsGeneric(t *testing.T) {
	srv, sqlDB := newServer(t, nil)
	notebook := &notebookTable{entries: []string{"first"}}
	srv.tables = append(srv.tables, notebook)
	h := srv.Handler()
	c := adminSession(t, h, sqlDB)

	full := tableFullPage(t, h, c, "/admin/tables/notebook")
	if !strings.Contains(full, "<h1>Notebook</h1>") {
		t.Errorf("generic page title missing:\n%s", full)
	}
	body := tableBody(t, h, c, "/admin/tables/notebook")
	for _, want := range []string{"first", "<th>id</th>", "<th>text</th>", "Showing 1–1 of 1 records."} {
		if !strings.Contains(body, want) {
			t.Errorf("generic grid missing %q:\n%s", want, body)
		}
	}

	// The new-record form comes from the adapter's defaults.
	newForm := tableBody(t, h, c, "/admin/tables/notebook/new")
	if !strings.Contains(newForm, `value="note"`) {
		t.Errorf("adapter defaults missing from the new form:\n%s", newForm)
	}

	rec := serve(h, adminRequest(http.MethodPost, "/admin/tables/notebook", url.Values{"text": {""}}, c))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Text is required.") {
		t.Errorf("field error missing:\n%s", rec.Body.String())
	}

	if rec := serve(h, adminRequest(http.MethodPost, "/admin/tables/notebook", url.Values{"text": {"second"}}, c)); rec.Code != http.StatusOK {
		t.Fatalf("create: status = %d, want 200", rec.Code)
	}
	if len(notebook.entries) != 2 || notebook.entries[1] != "second" {
		t.Errorf("entries = %v, want the new one appended", notebook.entries)
	}
}

func TestTablePagerURLs(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	setPageSize(t, h, c, "5")

	body := tableBody(t, h, c, timezonesPath)
	if !strings.Contains(body, `hx-get="`+timezonesPath+`?page=2"`) {
		t.Errorf("next-page URL missing:\n%s", body)
	}
	second := tableBody(t, h, c, timezonesPath+"?page=2")
	if !strings.Contains(second, `hx-get="`+timezonesPath+`?page=1"`) {
		t.Errorf("previous-page URL missing:\n%s", second)
	}
}

func TestTableSearchFormIsLeftOfAddRecord(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	body := tableFullPage(t, h, c, timezonesPath)
	searchAt := strings.Index(body, `data-testid="table-search"`)
	addAt := strings.Index(body, `data-testid="add-record"`)
	if searchAt < 0 || addAt < 0 {
		t.Fatalf("search form or Add record missing")
	}
	if searchAt > addAt {
		t.Error("the search form should come before the Add record button")
	}
	for _, want := range []string{
		`id="table-search-input" name="q"`,
		`data-testid="search-button">Search</button>`,
		`data-clear-search`,
		`hx-get="` + timezonesPath + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("search form missing %q", want)
		}
	}
}

func TestTableSearchFiltersRows(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	body := tableBody(t, h, c, timezonesPath+"?q=Asia")
	if n := strings.Count(body, `data-testid="table-row"`); n != 3 {
		t.Errorf("rows = %d, want 3 matching Asia:\n%s", n, body)
	}
	if !strings.Contains(body, "Showing 1–3 of 3 records.") {
		t.Errorf("filtered count missing:\n%s", body)
	}
	for _, want := range []string{"India", "China", "Philippines"} {
		if !strings.Contains(body, want) {
			t.Errorf("match %q missing:\n%s", want, body)
		}
	}
	if strings.Contains(body, "New Zealand") || strings.Contains(body, "UTC") {
		t.Error("non-matching records should be hidden")
	}

	// A normal page load shows the search text in the box.
	full := tableFullPage(t, h, c, timezonesPath+"?q=Asia")
	if !strings.Contains(full, `value="Asia"`) {
		t.Error("the search box should keep the active search text")
	}

	// The filter is case-insensitive for ASCII.
	lower := tableBody(t, h, c, timezonesPath+"?q=asia")
	if n := strings.Count(lower, `data-testid="table-row"`); n != 3 {
		t.Errorf("case-insensitive rows = %d, want 3", n)
	}
}

func TestTableSearchPreservedInLinks(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	setPageSize(t, h, c, "5")

	body := tableBody(t, h, c, timezonesPath+"?q=a")
	if !strings.Contains(body, "?page=2&amp;q=a") && !strings.Contains(body, "?page=2&q=a") {
		t.Errorf("next-page URL should keep the search:\n%s", body)
	}
	if !strings.Contains(body, "/edit?page=1&amp;q=a") && !strings.Contains(body, "/edit?page=1&q=a") {
		t.Errorf("edit URL should keep the search:\n%s", body)
	}

	second := tableBody(t, h, c, timezonesPath+"?q=a&page=2")
	if n := strings.Count(second, `data-testid="table-row"`); n == 0 {
		t.Errorf("page 2 of the search has no rows:\n%s", second)
	}
	if !strings.Contains(second, "q=a") {
		t.Errorf("pager URLs on page 2 should keep the search:\n%s", second)
	}
}

func TestTableSearchNoMatches(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	body := tableBody(t, h, c, timezonesPath+"?q=Nowhere")
	if !strings.Contains(body, "No records found.") {
		t.Errorf("no-match message missing:\n%s", body)
	}
	if strings.Contains(body, `data-testid="pager"`) {
		t.Error("no matches should show no pager")
	}

	// An empty table without a search keeps the old message.
	exec(t, sqlDB, `DELETE FROM timezones`)
	if empty := tableBody(t, h, c, timezonesPath); !strings.Contains(empty, "No records yet.") {
		t.Errorf("empty table message missing:\n%s", empty)
	}
}
