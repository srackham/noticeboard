package web

import (
	"context"
	"errors"
	"net/http/httptest"
	osexec "os/exec"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// browserContext returns a context for a headless Chrome browser. The test is
// skipped when Chrome or Chromium is not installed, or with -short, so the rest
// of the suite still runs on machines without a browser.
func browserContext(t *testing.T) context.Context {
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
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
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
	deadline := time.Now().Add(10 * time.Second)
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
