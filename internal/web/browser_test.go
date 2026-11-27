package web

import (
	"context"
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
