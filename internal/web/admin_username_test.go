package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginFormAsksForUsername(t *testing.T) {
	h, _ := newTestServer(t)
	body := serve(h, httptest.NewRequest(http.MethodGet, "/admin/login", nil)).Body.String()

	for _, want := range []string{
		`<label for="username">Username</label>`,
		`<input id="username" name="username" type="text"`,
		`autocomplete="username"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("login form missing %q", want)
		}
	}
	if strings.Contains(body, `type="email"`) || strings.Contains(body, `name="email"`) {
		t.Error("login form still asks for an email address")
	}
}

func TestHintShowsUsernameCommand(t *testing.T) {
	h, _ := newTestServer(t)
	body := serve(h, httptest.NewRequest(http.MethodGet, "/admin/login", nil)).Body.String()
	if !strings.Contains(body, "noticeboard admin init &lt;username&gt;") {
		t.Errorf("hint does not name the username command:\n%s", body)
	}
}

func TestLoginWithUsernameContainingSpaceFails(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	rec := login(h, "Admin Person", adminPassword)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), wrongMessage) {
		t.Errorf("generic message missing:\n%s", rec.Body.String())
	}
	if sessionCookie(rec) != nil {
		t.Error("a username with a space started a session")
	}
}

func TestLoginWithEmailStyleUsernameStillWorks(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	rec := serve(h, formRequest("/admin/login", url.Values{
		"username": {"  ADMIN@example.com "},
		"password": {adminPassword},
	}, true))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}
