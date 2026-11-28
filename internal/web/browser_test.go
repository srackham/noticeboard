package web

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	osexec "os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// browserContext returns a context for a headless Chrome browser. The test is
// skipped when Chrome or Chromium is not installed, or with -short, so the rest
// of the suite still runs on machines without a browser.
func browserContext(t *testing.T) context.Context {
	t.Helper()
	return browserContextTimeout(t, 60*time.Second)
}

// browserContextTimeout is browserContext with a custom lifetime for the
// browser, for tests that have to wait for a slow timer.
func browserContextTimeout(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser test skipped with -short")
	}
	found := false
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if _, err := osexec.LookPath(name); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Skip("Chrome or Chromium not found")
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		// The sandbox needs user namespaces, which are not available everywhere.
		chromedp.Flag("no-sandbox", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(cancelAlloc)
	ctx, cancelBrowser := chromedp.NewContext(allocCtx)
	t.Cleanup(cancelBrowser)
	ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
	t.Cleanup(cancelTimeout)
	return ctx
}

// TestClocksPageRendersInBrowser loads /clocks in Chrome and checks the rendered
// page: the document title, the nine tiles with their times, and the colours the
// stylesheet derives from the settings' CSS custom properties.
func TestClocksPageRendersInBrowser(t *testing.T) {
	h, _ := newTestServer(t)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)

	if _, err := chromedp.Run(ctx, chromedp.Navigate(ts.URL+"/clocks")); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Do(ctx, chromedp.WaitVisible(`[data-testid="clocks"]`)); err != nil {
		t.Fatalf("clocks grid not visible: %v", err)
	}

	docTitle, err := chromedp.Run(ctx, chromedp.Title())
	if err != nil {
		t.Fatalf("title: %v", err)
	}
	if docTitle != "World Clocks" {
		t.Errorf("document title = %q, want %q", docTitle, "World Clocks")
	}

	label, err := chromedp.Run(ctx, chromedp.Text(`[data-zone="UTC"] .label`))
	if err != nil {
		t.Fatalf("UTC label: %v", err)
	}
	if label != "UTC" {
		t.Errorf("UTC label = %q, want %q", label, "UTC")
	}

	timeText, err := chromedp.Run(ctx, chromedp.Text(`[data-zone="UTC"] time`))
	if err != nil {
		t.Fatalf("UTC time: %v", err)
	}
	if timeText != "14:05" {
		t.Errorf("UTC time = %q, want %q (the seeded time)", timeText, "14:05")
	}

	tiles, err := chromedp.Run(ctx, chromedp.Evaluate[int](
		`document.querySelectorAll('[data-testid="clock"]').length`))
	if err != nil {
		t.Fatalf("count tiles: %v", err)
	}
	if tiles != 9 {
		t.Errorf("tiles = %d, want 9", tiles)
	}

	// The colours come from the settings row through CSS custom properties, so
	// they only appear once the stylesheet has been applied.
	colours, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](`
		[
			getComputedStyle(document.querySelector('[data-zone="UTC"] .value')).color,
			getComputedStyle(document.querySelector('[data-zone="Asia/Kolkata"] .value')).color
		]`))
	if err != nil {
		t.Fatalf("read colours: %v", err)
	}
	if want := "rgb(178, 34, 34)"; colours[0] != want {
		t.Errorf("UTC clock colour = %q, want %q", colours[0], want)
	}
	if want := "rgb(34, 139, 34)"; colours[1] != want {
		t.Errorf("non-UTC clock colour = %q, want %q", colours[1], want)
	}
}

// TestClocksPagePollsInBrowser checks that the HTMX poll keeps the page current:
// a settings change made while the page is open appears without a reload.
func TestClocksPagePollsInBrowser(t *testing.T) {
	srv, sqlDB := newServer(t, nil)
	exec(t, sqlDB, `UPDATE settings SET refresh_seconds = 1`)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx := browserContext(t)

	if _, err := chromedp.Run(ctx, chromedp.Navigate(ts.URL+"/clocks")); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := chromedp.Do(ctx, chromedp.WaitVisible(`[data-testid="title"]`)); err != nil {
		t.Fatalf("title not visible: %v", err)
	}

	// Change the title behind the page's back; the next poll must pick it up.
	exec(t, sqlDB, `UPDATE settings SET title = 'Harbour'`)
	_, err := chromedp.Run(ctx, chromedp.Poll[bool](
		`document.querySelector('[data-testid="title"]').textContent === 'Harbour'`,
		chromedp.WithPollingInterval(100*time.Millisecond),
		chromedp.WithPollingTimeout(10*time.Second),
	))
	if err != nil {
		t.Fatalf("title did not update from the poll: %v", err)
	}

	title, err := chromedp.Run(ctx, chromedp.Text(`[data-testid="title"]`))
	if err != nil {
		t.Fatalf("read title: %v", err)
	}
	if title != "Harbour" {
		t.Errorf("title = %q, want %q", title, "Harbour")
	}
}

// waitFor polls the JavaScript expression in the page until it is true. Errors
// are ignored and retried, because a navigation in flight destroys the
// JavaScript execution context that the next evaluation needs.
func waitFor(context context.Context, expression string) error {
	return waitForTimeout(context, expression, 10*time.Second)
}

// waitForTimeout is waitFor with a custom timeout.
func waitForTimeout(context context.Context, expression string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ok, err := chromedp.Run(context, chromedp.Evaluate[bool](expression))
		if err == nil && ok {
			return nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("timed out waiting for " + expression)
}

// loginInBrowser opens the login page, fills in the form and presses the Log in
// button, then waits for the dashboard. It leaves the browser logged in.
func loginInBrowser(t *testing.T, ctx context.Context, baseURL string) {
	t.Helper()
	if _, err := chromedp.Run(ctx, chromedp.Navigate(baseURL+"/admin/login")); err != nil {
		t.Fatalf("navigate to the login page: %v", err)
	}
	if err := chromedp.Do(ctx,
		chromedp.WaitVisible(`#login-form`),
		chromedp.SendKeys(`#username`, adminUsername),
		chromedp.SendKeys(`#password`, adminPassword),
		chromedp.Click(`#login-form button[type="submit"]`),
	); err != nil {
		t.Fatalf("log in: %v", err)
	}
	// The login response carries HX-Redirect: /admin, so HTMX navigates there.
	if err := waitFor(ctx, `location.pathname === "/admin" &&
		document.querySelector('[data-testid="logout"]') !== null`); err != nil {
		t.Fatalf("dashboard did not load after logging in: %v", err)
	}
}

// TestAdminLoginInBrowser logs in through the login page's Log in button.
func TestAdminLoginInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)

	loginInBrowser(t, ctx, ts.URL)

	menu, err := chromedp.Run(ctx, chromedp.Text(`[data-testid="menu-administration"]`))
	if err != nil {
		t.Fatalf("read the admin menu: %v", err)
	}
	if menu != "Administration" {
		t.Errorf("menu item = %q, want %q", menu, "Administration")
	}
	if n := countRows(t, sqlDB, "sessions"); n != 1 {
		t.Errorf("sessions rows = %d, want 1", n)
	}
}

// TestAdminLogoutInBrowser logs in, then logs out through the admin menu's
// Log out button, and checks that the session is gone.
func TestAdminLogoutInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)

	loginInBrowser(t, ctx, ts.URL)

	if err := chromedp.Do(ctx, chromedp.Click(`[data-testid="logout"]`)); err != nil {
		t.Fatalf("click Log out: %v", err)
	}
	// The logout response carries HX-Redirect: /admin/login.
	if err := waitFor(ctx, `location.pathname === "/admin/login" &&
		document.querySelector('#login-form') !== null`); err != nil {
		t.Fatalf("login page did not load after logging out: %v", err)
	}

	// The session is gone: a fresh request for the dashboard goes to the login page.
	if _, err := chromedp.Run(ctx, chromedp.Navigate(ts.URL+"/admin")); err != nil {
		t.Fatalf("navigate to the dashboard: %v", err)
	}
	if err := waitFor(ctx, `location.pathname === "/admin/login"`); err != nil {
		t.Fatalf("dashboard was reachable after logging out: %v", err)
	}
	if n := countRows(t, sqlDB, "sessions"); n != 0 {
		t.Errorf("sessions rows = %d, want 0", n)
	}
}

// zoneRowExpr is a JavaScript expression that finds the clock row for a zone. The
// rows carry no zone attribute, so the zone cell is compared.
func zoneRowExpr(zone string) string {
	return fmt.Sprintf(`[...document.querySelectorAll('[data-testid="zone-row"]')].find(r => r.cells[1].textContent.trim() === %q)`, zone)
}

// clickRowButton clicks the button with the given text in the row for zone.
func clickRowButton(ctx context.Context, zone, text string) error {
	expression := fmt.Sprintf(`(() => {
		const row = %s;
		if (!row) { return "no row for zone " + %q; }
		const button = [...row.querySelectorAll("button")].find(b => b.textContent.trim() === %q);
		if (!button) { return "no " + %q + " button in the row"; }
		button.click();
		return "";
	})()`, zoneRowExpr(zone), zone, text, text)
	msg, err := chromedp.Run(ctx, chromedp.Evaluate[string](expression))
	if err != nil {
		return err
	}
	if msg != "" {
		return errors.New(msg)
	}
	return nil
}

// setField sets the value of the form field with the given id.
func setField(ctx context.Context, id, value string) error {
	expression := fmt.Sprintf(`(() => {
		const field = document.getElementById(%q);
		if (!field) { return "no field " + %q; }
		field.value = %q;
		return "";
	})()`, id, id, value)
	msg, err := chromedp.Run(ctx, chromedp.Evaluate[string](expression))
	if err != nil {
		return err
	}
	if msg != "" {
		return errors.New(msg)
	}
	return nil
}

// clockRows returns the number of clock rows on the dashboard.
func clockRows(ctx context.Context) (int, error) {
	return chromedp.Run(ctx, chromedp.Evaluate[int](`document.querySelectorAll('[data-testid="zone-row"]').length`))
}

// statusColour returns the computed colour of the settings form's status line.
func statusColour(ctx context.Context) (string, error) {
	return chromedp.Run(ctx, chromedp.Evaluate[string](`getComputedStyle(document.getElementById("status-line")).color`))
}

// settingsStatus is the condition for a save outcome shown in the status line:
// the class and the text must both match.
func settingsStatus(class, text string) string {
	return fmt.Sprintf(`(() => {
		const line = document.getElementById("status-line");
		return !!line && line.classList.contains(%q) &&
			document.getElementById("status-text").textContent === %q;
	})()`, class, text)
}

// saveSettings presses the settings form's Save settings button.
func saveSettings(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Do(ctx, chromedp.Click(`#settings-form button[type="submit"]`)); err != nil {
		t.Fatalf("save the settings: %v", err)
	}
}

// TestAdminSettingsStatusInBrowser checks the save outcome in the status line: a
// green "Settings saved" after a valid save, and a red "Failed to save settings"
// with the field error after a rejected one.
func TestAdminSettingsStatusInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)
	loginInBrowser(t, ctx, ts.URL)

	if err := setField(ctx, "title", "Harbour"); err != nil {
		t.Fatalf("set the title: %v", err)
	}
	saveSettings(t, ctx)
	if err := waitFor(ctx, settingsStatus("status-ok", "Settings saved")); err != nil {
		t.Fatalf("green success status not shown: %v", err)
	}
	if colour, err := statusColour(ctx); err != nil {
		t.Fatalf("read the status colour: %v", err)
	} else if colour != "rgb(46, 125, 50)" {
		t.Errorf("success colour = %q, want %q", colour, "rgb(46, 125, 50)")
	}

	// An 81-character title is rejected with 422, so the form comes back red.
	if err := setField(ctx, "title", strings.Repeat("x", 81)); err != nil {
		t.Fatalf("set an invalid title: %v", err)
	}
	saveSettings(t, ctx)
	if err := waitFor(ctx, settingsStatus("status-error", "Failed to save settings")); err != nil {
		t.Fatalf("red failure status not shown: %v", err)
	}
	if colour, err := statusColour(ctx); err != nil {
		t.Fatalf("read the failure colour: %v", err)
	} else if colour != "rgb(178, 34, 34)" {
		t.Errorf("failure colour = %q, want %q", colour, "rgb(178, 34, 34)")
	}
	ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`document.querySelector('[data-testid="error-title"]') !== null`))
	if err != nil {
		t.Fatalf("look for the field error: %v", err)
	}
	if !ok {
		t.Error("field error for the title is missing")
	}
}

// TestAdminClockAddInBrowser adds a clock through the Add clock button and the
// form it loads, and checks the list and the form region it leaves behind.
func TestAdminClockAddInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	// The seed has nine enabled clocks; free a slot so the new one is accepted.
	exec(t, sqlDB, `UPDATE timezones SET enabled = 0 WHERE zone = 'UTC'`)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)
	loginInBrowser(t, ctx, ts.URL)

	if err := chromedp.Do(ctx, chromedp.Click(`[data-testid="add-clock"]`)); err != nil {
		t.Fatalf("click Add clock: %v", err)
	}
	if err := waitFor(ctx, `document.querySelector('#clock-form #zone-form') !== null`); err != nil {
		t.Fatalf("the new-clock form did not load: %v", err)
	}
	if err := setField(ctx, "label", "Japan"); err != nil {
		t.Fatalf("set the label: %v", err)
	}
	if err := setField(ctx, "zone", "Asia/Tokyo"); err != nil {
		t.Fatalf("set the zone: %v", err)
	}
	if err := chromedp.Do(ctx, chromedp.Click(`#zone-form button[type="submit"]`)); err != nil {
		t.Fatalf("save the new clock: %v", err)
	}

	if err := waitFor(ctx, `document.querySelectorAll('[data-testid="zone-row"]').length === 10 &&
		document.querySelector('[data-testid="enabled-count"]').textContent.trim() === "9 of 9 clocks enabled."`); err != nil {
		t.Fatalf("the new clock did not appear: %v", err)
	}
	if ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`document.getElementById("clock-form").children.length === 0`)); err != nil || !ok {
		t.Errorf("the form region was not emptied (%v)", err)
	}
	var label string
	if err := sqlDB.QueryRow(`SELECT label FROM timezones WHERE zone = 'Asia/Tokyo'`).Scan(&label); err != nil {
		t.Fatalf("read the new clock: %v", err)
	}
	if label != "Japan" {
		t.Errorf("stored label = %q, want %q", label, "Japan")
	}
}

// TestAdminClockEditInBrowser edits a clock through the row's Edit button and
// checks that the list refreshes in place.
func TestAdminClockEditInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)
	loginInBrowser(t, ctx, ts.URL)

	if err := clickRowButton(ctx, "Asia/Kolkata", "Edit"); err != nil {
		t.Fatalf("click Edit: %v", err)
	}
	if err := waitFor(ctx, `document.querySelector('#clock-form #zone-form') !== null &&
		document.querySelector('#zone-form h3').textContent.trim() === "Edit clock"`); err != nil {
		t.Fatalf("the edit form did not load: %v", err)
	}
	if err := setField(ctx, "label", "India Standard"); err != nil {
		t.Fatalf("set the label: %v", err)
	}
	if err := chromedp.Do(ctx, chromedp.Click(`#zone-form button[type="submit"]`)); err != nil {
		t.Fatalf("save the clock: %v", err)
	}

	if err := waitFor(ctx, fmt.Sprintf(`%s.cells[0].textContent.trim() === %q`, zoneRowExpr("Asia/Kolkata"), "India Standard")); err != nil {
		t.Fatalf("the edited label did not appear: %v", err)
	}
	var label string
	if err := sqlDB.QueryRow(`SELECT label FROM timezones WHERE zone = 'Asia/Kolkata'`).Scan(&label); err != nil {
		t.Fatalf("read the clock: %v", err)
	}
	if label != "India Standard" {
		t.Errorf("stored label = %q, want %q", label, "India Standard")
	}
}

// TestAdminClockToggleInBrowser disables and re-enables a clock, checking the row
// class, the status cell and the enabled count each time.
func TestAdminClockToggleInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)
	loginInBrowser(t, ctx, ts.URL)

	if err := clickRowButton(ctx, "Asia/Kolkata", "Disable"); err != nil {
		t.Fatalf("click Disable: %v", err)
	}
	if err := waitFor(ctx, fmt.Sprintf(`%s.classList.contains("disabled") &&
		%s.cells[2].textContent.trim() === "Disabled" &&
		document.querySelector('[data-testid="enabled-count"]').textContent.trim() === "8 of 9 clocks enabled."`,
		zoneRowExpr("Asia/Kolkata"), zoneRowExpr("Asia/Kolkata"))); err != nil {
		t.Fatalf("the clock was not disabled: %v", err)
	}
	var enabled int
	if err := sqlDB.QueryRow(`SELECT enabled FROM timezones WHERE zone = 'Asia/Kolkata'`).Scan(&enabled); err != nil {
		t.Fatalf("read the clock: %v", err)
	}
	if enabled != 0 {
		t.Errorf("stored enabled = %d, want 0", enabled)
	}

	if err := clickRowButton(ctx, "Asia/Kolkata", "Enable"); err != nil {
		t.Fatalf("click Enable: %v", err)
	}
	if err := waitFor(ctx, fmt.Sprintf(`!%s.classList.contains("disabled") &&
		document.querySelector('[data-testid="enabled-count"]').textContent.trim() === "9 of 9 clocks enabled."`,
		zoneRowExpr("Asia/Kolkata"))); err != nil {
		t.Fatalf("the clock was not enabled again: %v", err)
	}
}

// TestAdminClockMoveInBrowser moves a clock up and back down, checking the list
// order and the disabled state at the ends of the list.
func TestAdminClockMoveInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)
	loginInBrowser(t, ctx, ts.URL)

	if err := clickRowButton(ctx, "Pacific/Auckland", "Up"); err != nil {
		t.Fatalf("click Up: %v", err)
	}
	if err := waitFor(ctx, `document.querySelectorAll('[data-testid="zone-row"]')[0].cells[1].textContent.trim() === "Pacific/Auckland"`); err != nil {
		t.Fatalf("the clock did not move up: %v", err)
	}
	if ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`[...document.querySelectorAll('[data-testid="zone-row"]')[0].querySelectorAll("button")]
		.find(b => b.textContent.trim() === "Up").disabled`)); err != nil || !ok {
		t.Errorf("Up at the top of the list is not disabled (%v)", err)
	}

	if err := clickRowButton(ctx, "Pacific/Auckland", "Down"); err != nil {
		t.Fatalf("click Down: %v", err)
	}
	if err := waitFor(ctx, `document.querySelectorAll('[data-testid="zone-row"]')[0].cells[1].textContent.trim() === "UTC"`); err != nil {
		t.Fatalf("the clock did not move down again: %v", err)
	}
}

// TestAdminClockDeleteInBrowser answers the Delete button's hx-confirm dialog
// both ways: dismissing keeps the clock, accepting deletes it.
func TestAdminClockDeleteInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)
	loginInBrowser(t, ctx, ts.URL)

	// The dialog blocks the page, so it must be answered from a second goroutine.
	// Events subscribes as it is called, before the click, so no event is lost.
	var (
		mu       sync.Mutex
		messages []string
		accept   atomic.Bool
	)
	dialogs := chromedp.Events(ctx, page.JavascriptDialogOpening)
	go func() {
		for ev, err := range dialogs {
			if err != nil {
				return
			}
			mu.Lock()
			messages = append(messages, ev.Message)
			want := accept.Load()
			mu.Unlock()
			_, _ = chromedp.Call(ctx, page.HandleJavaScriptDialog,
				page.HandleJavaScriptDialogParams{Accept: want})
		}
	}()

	if err := clickRowButton(ctx, "Europe/Kyiv", "Delete"); err != nil {
		t.Fatalf("click Delete: %v", err)
	}
	mu.Lock()
	got := append([]string(nil), messages...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "Delete Ukraine?" {
		t.Fatalf("dialog messages = %q, want [\"Delete Ukraine?\"]", got)
	}
	if n, err := clockRows(ctx); err != nil || n != 9 {
		t.Errorf("rows after dismissing = %d (%v), want 9", n, err)
	}
	if n := countRows(t, sqlDB, "timezones"); n != 9 {
		t.Errorf("timezones rows after dismissing = %d, want 9", n)
	}

	accept.Store(true)
	if err := clickRowButton(ctx, "Europe/Kyiv", "Delete"); err != nil {
		t.Fatalf("click Delete again: %v", err)
	}
	if err := waitFor(ctx, `document.querySelectorAll('[data-testid="zone-row"]').length === 8`); err != nil {
		t.Fatalf("the clock was not deleted: %v", err)
	}
	if n := countRows(t, sqlDB, "timezones"); n != 8 {
		t.Errorf("timezones rows = %d, want 8", n)
	}
}

// TestAdminMenuNavigationInBrowser follows the sidebar links to the timezones
// table page and back, checking the current-page marker on the way.
func TestAdminMenuNavigationInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)
	loginInBrowser(t, ctx, ts.URL)

	if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`document.querySelector('[data-testid="menu-timezones"]').click(), true`)); err != nil {
		t.Fatalf("click the timezones menu item: %v", err)
	}
	if err := waitFor(ctx, `location.pathname === "/admin/tables/timezones" &&
		document.querySelector('#table-grid') !== null`); err != nil {
		t.Fatalf("the timezones table page did not load: %v", err)
	}
	if ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`document.querySelector('[data-testid="menu-timezones"]').getAttribute("aria-current") === "page"`)); err != nil || !ok {
		t.Errorf("the timezones item is not marked current (%v)", err)
	}

	if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`document.querySelector('[data-testid="menu-administration"]').click(), true`)); err != nil {
		t.Fatalf("click the Administration menu item: %v", err)
	}
	if err := waitFor(ctx, `location.pathname === "/admin" &&
		document.querySelector('#settings-form') !== null`); err != nil {
		t.Fatalf("the dashboard did not load: %v", err)
	}
	if ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`document.querySelector('[data-testid="menu-administration"]').getAttribute("aria-current") === "page"`)); err != nil || !ok {
		t.Errorf("the Administration item is not marked current (%v)", err)
	}
}

// TestAdminSettingsReachClocksInBrowser saves a new title on the dashboard, then
// loads the clocks page and checks that the same settings row is used there.
func TestAdminSettingsReachClocksInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContext(t)
	loginInBrowser(t, ctx, ts.URL)

	if err := setField(ctx, "title", "Harbour"); err != nil {
		t.Fatalf("set the title: %v", err)
	}
	saveSettings(t, ctx)
	if err := waitFor(ctx, settingsStatus("status-ok", "Settings saved")); err != nil {
		t.Fatalf("the save was not confirmed: %v", err)
	}

	if _, err := chromedp.Run(ctx, chromedp.Navigate(ts.URL+"/clocks")); err != nil {
		t.Fatalf("navigate to the clocks page: %v", err)
	}
	if err := waitFor(ctx, `document.querySelector('[data-testid="title"]').textContent === "Harbour"`); err != nil {
		t.Fatalf("the clocks page does not show the new title: %v", err)
	}
}

// TestAdminIdleLogoutInBrowser waits for the shortest supported login timeout,
// 0.5 minutes, twice over, so it takes a little over a minute.
func TestAdminIdleLogoutInBrowser(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	exec(t, sqlDB, `UPDATE settings SET login_timeout = 0.5`)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx := browserContextTimeout(t, 150*time.Second)

	loginInBrowser(t, ctx, ts.URL)

	// Activity before the timeout is due must keep the session alive.
	time.Sleep(20 * time.Second)
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`document.dispatchEvent(new MouseEvent("mousemove")), true`)); err != nil {
		t.Fatalf("dispatch activity: %v", err)
	}
	time.Sleep(13 * time.Second) // 33 s after loading, past the original 30 s deadline
	ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`location.pathname === "/admin"`))
	if err != nil {
		t.Fatalf("read the location: %v", err)
	}
	if !ok {
		t.Fatal("the session ended even though there was activity")
	}

	// With no further activity the idle log-out fires about 30 s later.
	if err := waitForTimeout(ctx,
		`location.pathname === "/admin/login" && document.querySelector("#login-form") !== null`,
		45*time.Second); err != nil {
		t.Fatalf("the idle log-out did not happen: %v", err)
	}
	if n := countRows(t, sqlDB, "sessions"); n != 0 {
		t.Errorf("sessions after the idle log-out = %d, want 0", n)
	}
}
