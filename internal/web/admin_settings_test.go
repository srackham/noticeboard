package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// validSettingsForm is a submission that passes validation.
func validSettingsForm() url.Values {
	return url.Values{
		"title":             {"Harbour"},
		"refresh_seconds":   {"10"},
		"colour_background": {"#FFFFFF"},
		"colour_label":      {"#4169e1"},
		"colour_clock":      {"#228b22"},
		"colour_utc":        {"#b22222"},
		"table_page_size":   {"20"},
	}
}

func TestDashboardShowsSettingsForm(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin", nil), c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="settings"`,
		`id="settings-form"`,
		`hx-post="/admin/settings"`,
		`name="title" type="text" maxlength="80" value="World Clocks"`,
		`name="refresh_seconds" type="number" min="1" max="60" step="1" value="5"`,
		`name="colour_background" type="color" value="#d9d9d9"`,
		`name="colour_label" type="color" value="#4169e1"`,
		`name="colour_clock" type="color" value="#228b22"`,
		`name="colour_utc" type="color" value="#b22222"`,
		`name="table_page_size" type="number" min="5" max="100" step="1" value="20"`,
		`<legend>Admin tables</legend>`,
		`data-testid="tables-link"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestSaveSettingsStoresAndShowsSaved(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)

	rec := serve(h, adminRequest(http.MethodPost, "/admin/settings", validSettingsForm(), c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="settings-saved"`) || !strings.Contains(body, "Saved.") {
		t.Errorf("saved message missing:\n%s", body)
	}
	if !strings.Contains(body, `id="settings-form"`) {
		t.Error("response should return the settings form")
	}

	var title, bg string
	var refresh int
	if err := sqlDB.QueryRow(`SELECT title, refresh_seconds, colour_background FROM settings WHERE id = 1`).
		Scan(&title, &refresh, &bg); err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if title != "Harbour" || refresh != 10 || bg != "#ffffff" {
		t.Errorf("stored = %q, %d, %q; want Harbour, 10, #ffffff (lowercase)", title, refresh, bg)
	}

	// The board reads the same row, so the new title appears on the next poll.
	if board := serve(h, httptest.NewRequest(http.MethodGet, "/board", nil)).Body.String(); !strings.Contains(board, "Harbour") {
		t.Error("board does not show the saved title")
	}
}

func TestSaveSettingsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		value     string
		wantError string
	}{
		{name: "refresh too large", field: "refresh_seconds", value: "61", wantError: "Refresh must be"},
		{name: "refresh not a number", field: "refresh_seconds", value: "soon", wantError: "Refresh must be"},
		{name: "background short form", field: "colour_background", value: "#abc", wantError: "Background colour must be"},
		{name: "utc not a colour", field: "colour_utc", value: "red", wantError: "UTC colour must be"},
		{name: "title too long", field: "title", value: strings.Repeat("x", 81), wantError: "Title must be 80 characters or fewer."},
		{name: "page size too small", field: "table_page_size", value: "4", wantError: "Rows per page must be"},
		{name: "page size too large", field: "table_page_size", value: "101", wantError: "Rows per page must be"},
		{name: "page size not a number", field: "table_page_size", value: "many", wantError: "Rows per page must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, sqlDB := newTestServer(t)
			c := adminSession(t, h, sqlDB)
			form := validSettingsForm()
			form.Set(tt.field, tt.value)

			rec := serve(h, adminRequest(http.MethodPost, "/admin/settings", form, c))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.wantError) {
				t.Errorf("message %q missing:\n%s", tt.wantError, body)
			}
			if !strings.Contains(body, `id="settings-form"`) {
				t.Error("422 should re-render the settings form")
			}
			if strings.Contains(body, "Saved.") {
				t.Error("rejected input reported as saved")
			}

			var title string
			sqlDB.QueryRow(`SELECT title FROM settings WHERE id = 1`).Scan(&title)
			if title != "World Clocks" {
				t.Errorf("stored title = %q after rejected save, want unchanged", title)
			}
		})
	}
}

func TestRejectedSettingsKeepTypedValues(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	form := validSettingsForm()
	form.Set("refresh_seconds", "soon")
	form.Set("title", "My typed title")

	body := serve(h, adminRequest(http.MethodPost, "/admin/settings", form, c)).Body.String()
	if !strings.Contains(body, `value="soon"`) || !strings.Contains(body, `value="My typed title"`) {
		t.Errorf("typed values not kept:\n%s", body)
	}
}

func TestBlankTitleHidesBoardHeader(t *testing.T) {
	h, sqlDB := newTestServer(t)
	c := adminSession(t, h, sqlDB)
	form := validSettingsForm()
	form.Set("title", "   ")

	if rec := serve(h, adminRequest(http.MethodPost, "/admin/settings", form, c)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if board := serve(h, httptest.NewRequest(http.MethodGet, "/board", nil)).Body.String(); strings.Contains(board, `data-testid="title"`) {
		t.Error("blank title should hide the header")
	}
}

func TestSettingsNeedSessionAndHTMX(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	req := httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(validSettingsForm().Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	if rec := serve(h, req); rec.Code != http.StatusUnauthorized {
		t.Errorf("without session: status = %d, want 401", rec.Code)
	}

	c := sessionCookie(login(h, adminUsername, adminPassword))
	req = withCookie(httptest.NewRequest(http.MethodPost, "/admin/settings", strings.NewReader(validSettingsForm().Encode())), c)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := serve(h, req); rec.Code != http.StatusForbidden {
		t.Errorf("without HX-Request: status = %d, want 403", rec.Code)
	}
}
