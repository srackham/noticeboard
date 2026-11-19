package web

import (
	"context"
	"crypto/tls"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"noticeboard/internal/auditlog"
	"noticeboard/internal/auth"
)

const (
	adminUsername = "Admin@Example.com"
	adminPassword = "correct horse"
	wrongMessage  = "Username or password is incorrect."
)

// seedAdmin creates the administrator account used by the tests.
func seedAdmin(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if err := auth.UpsertAdmin(context.Background(), sqlDB, adminUsername, adminPassword); err != nil {
		t.Fatalf("UpsertAdmin: %v", err)
	}
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// formRequest builds a POST with a form body. With hx set it carries the HX-Request header.
func formRequest(path string, form url.Values, hx bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	return req
}

// login posts the login form as HTMX would.
func login(h http.Handler, username, password string) *httptest.ResponseRecorder {
	return serve(h, formRequest("/admin/login", url.Values{"username": {username}, "password": {password}}, true))
}

// sessionCookie returns the session cookie set by rec, or nil.
func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

// withCookie adds c to req when c is not nil.
func withCookie(req *http.Request, c *http.Cookie) *http.Request {
	if c != nil {
		req.AddCookie(c)
	}
	return req
}

func countRows(t *testing.T, sqlDB *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestLoginPageShowsForm(t *testing.T) {
	h, _ := newTestServer(t)
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/admin/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="login-form"`,
		`hx-post="/admin/login"`,
		`name="username"`,
		`name="password"`,
		`type="password"`,
		`<a class="button" href="/" data-testid="login-cancel">Cancel</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q", want)
		}
	}
}

func TestLoginPageHintOnlyWithoutAdmin(t *testing.T) {
	h, sqlDB := newTestServer(t)

	body := serve(h, httptest.NewRequest(http.MethodGet, "/admin/login", nil)).Body.String()
	if !strings.Contains(body, "noticeboard admin init &lt;username&gt;") {
		t.Errorf("hint missing with no admin:\n%s", body)
	}

	seedAdmin(t, sqlDB)
	body = serve(h, httptest.NewRequest(http.MethodGet, "/admin/login", nil)).Body.String()
	if strings.Contains(body, `data-testid="admin-hint"`) {
		t.Error("hint shown after an admin exists")
	}
}

func TestLoginSucceeds(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	rec := login(h, "  ADMIN@example.com ", adminPassword)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/admin" {
		t.Errorf("HX-Redirect = %q, want /admin", got)
	}

	c := sessionCookie(rec)
	if c == nil {
		t.Fatal("no session cookie set")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/admin" {
		t.Errorf("cookie = %+v, want HttpOnly, SameSite=Strict, Path=/admin", c)
	}
	if c.Secure {
		t.Error("cookie is Secure over plain HTTP")
	}
	if c.MaxAge != int(auth.SessionLifetime.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(auth.SessionLifetime.Seconds()))
	}
	if countRows(t, sqlDB, "sessions") != 1 {
		t.Errorf("sessions rows = %d, want 1", countRows(t, sqlDB, "sessions"))
	}

	dash := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin", nil), c))
	if dash.Code != http.StatusOK || !strings.Contains(dash.Body.String(), "Administration") {
		t.Errorf("dashboard with session: status %d", dash.Code)
	}
}

func TestLoginCookieIsSecureOverTLS(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	req := formRequest("/admin/login", url.Values{"username": {adminUsername}, "password": {adminPassword}}, true)
	req.TLS = &tls.ConnectionState{}
	c := sessionCookie(serve(h, req))
	if c == nil || !c.Secure {
		t.Errorf("cookie over TLS = %+v, want Secure", c)
	}
}

func TestLoginFailureIsGeneric(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	tests := []struct {
		name, username, password string
	}{
		{name: "wrong password", username: adminUsername, password: "wrong"},
		{name: "unknown username", username: "someone@example.com", password: adminPassword},
		{name: "empty fields", username: "", password: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := login(h, tt.username, tt.password)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), wrongMessage) {
				t.Errorf("message missing:\n%s", rec.Body.String())
			}
			if sessionCookie(rec) != nil {
				t.Error("failed login set a cookie")
			}
			if rec.Header().Get("HX-Redirect") != "" {
				t.Error("failed login redirected")
			}
		})
	}
	if n := countRows(t, sqlDB, "sessions"); n != 0 {
		t.Errorf("failed logins created %d sessions", n)
	}
}

func TestLoginFailureEchoesUsernameNotPassword(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	body := login(h, "Typo@Example.com", "secret-password").Body.String()
	if !strings.Contains(body, `value="Typo@Example.com"`) {
		t.Errorf("username not kept in the form:\n%s", body)
	}
	if strings.Contains(body, "secret-password") {
		t.Error("password echoed back into the form")
	}
}

func TestLoginRequiresHTMXHeader(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	req := formRequest("/admin/login", url.Values{"username": {adminUsername}, "password": {adminPassword}}, false)
	rec := serve(h, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if n := countRows(t, sqlDB, "sessions"); n != 0 {
		t.Errorf("request without HX-Request created %d sessions", n)
	}
}

func TestLoginRejectsOversizedBody(t *testing.T) {
	h, _ := newTestServer(t)
	big := url.Values{"username": {strings.Repeat("a", 70<<10)}}
	rec := serve(h, formRequest("/admin/login", big, true))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

func TestAdminPageNeedsSession(t *testing.T) {
	h, _ := newTestServer(t)

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Errorf("no session: status %d, Location %q; want 303 to /admin/login", rec.Code, rec.Header().Get("Location"))
	}

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("HX-Request", "true")
	rec = serve(h, req)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("HX-Redirect") != "/admin/login" {
		t.Errorf("HTMX without session: status %d, HX-Redirect %q; want 401 and /admin/login",
			rec.Code, rec.Header().Get("HX-Redirect"))
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	past := fixedNow.Add(-auth.SessionLifetime - time.Hour)
	token, err := auth.CreateSession(context.Background(), sqlDB, past)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	c := &http.Cookie{Name: sessionCookieName, Value: token}

	if rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin", nil), c)); rec.Code != http.StatusSeeOther {
		t.Errorf("expired session: status %d, want 303", rec.Code)
	}
	req := withCookie(httptest.NewRequest(http.MethodGet, "/admin", nil), c)
	req.Header.Set("HX-Request", "true")
	if rec := serve(h, req); rec.Code != http.StatusUnauthorized || rec.Header().Get("HX-Redirect") != "/admin/login" {
		t.Errorf("expired session via HTMX: status %d, HX-Redirect %q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
}

func TestLoginPageRedirectsWhenLoggedIn(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	c := sessionCookie(login(h, adminUsername, adminPassword))

	rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin/login", nil), c))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Errorf("status %d, Location %q; want 303 to /admin", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLogoutEndsSession(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	c := sessionCookie(login(h, adminUsername, adminPassword))

	req := withCookie(httptest.NewRequest(http.MethodPost, "/admin/logout", nil), c)
	req.Header.Set("HX-Request", "true")
	rec := serve(h, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/admin/login" {
		t.Errorf("HX-Redirect = %q, want /admin/login", got)
	}
	cleared := sessionCookie(rec)
	if cleared == nil || cleared.MaxAge >= 0 || cleared.Path != "/admin" {
		t.Errorf("logout cookie = %+v, want expired (Max-Age=0) with Path=/admin", cleared)
	}
	if n := countRows(t, sqlDB, "sessions"); n != 0 {
		t.Errorf("sessions after logout = %d, want 0", n)
	}
	if rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin", nil), c)); rec.Code != http.StatusSeeOther {
		t.Errorf("old cookie after logout: status %d, want 303", rec.Code)
	}
}

func TestLogoutRequiresSessionAndHTMX(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)
	c := sessionCookie(login(h, adminUsername, adminPassword))

	// Without HX-Request the logout is refused and the session survives.
	if rec := serve(h, withCookie(httptest.NewRequest(http.MethodPost, "/admin/logout", nil), c)); rec.Code != http.StatusForbidden {
		t.Errorf("logout without HX-Request: status %d, want 403", rec.Code)
	}
	if rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin", nil), c)); rec.Code != http.StatusOK {
		t.Errorf("session after refused logout: status %d, want 200", rec.Code)
	}

	// Without a session, logout redirects to the login page.
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	req.Header.Set("HX-Request", "true")
	if rec := serve(h, req); rec.Code != http.StatusUnauthorized || rec.Header().Get("HX-Redirect") != "/admin/login" {
		t.Errorf("logout without session: status %d, HX-Redirect %q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
}

func TestUnknownAdminPathNeedsSession(t *testing.T) {
	h, sqlDB := newTestServer(t)
	seedAdmin(t, sqlDB)

	if rec := serve(h, httptest.NewRequest(http.MethodGet, "/admin/nothing", nil)); rec.Code != http.StatusSeeOther {
		t.Errorf("unknown path without session: status %d, want 303", rec.Code)
	}
	c := sessionCookie(login(h, adminUsername, adminPassword))
	if rec := serve(h, withCookie(httptest.NewRequest(http.MethodGet, "/admin/nothing", nil), c)); rec.Code != http.StatusNotFound {
		t.Errorf("unknown path with session: status %d, want 404", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h, _ := newTestServer(t)
	for _, path := range []string{"/", "/board", "/admin/login"} {
		rec := serve(h, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", path, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options = %q", path, got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'; style-src 'self' 'unsafe-inline'" {
			t.Errorf("%s: Content-Security-Policy = %q", path, got)
		}
	}
}

// auditLines returns the lines written to the audit log at path.
func auditLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestAuditLogRecordsLoginsAndAdminChanges(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.log")
	audit, err := auditlog.Open(logPath)
	if err != nil {
		t.Fatalf("auditlog.Open: %v", err)
	}
	srv, sqlDB := newServer(t, audit)
	h := srv.Handler()
	seedAdmin(t, sqlDB)

	// GET requests are not logged.
	serve(h, httptest.NewRequest(http.MethodGet, "/", nil))
	serve(h, httptest.NewRequest(http.MethodGet, "/admin/login", nil))

	login(h, adminUsername, "wrong")
	rec := login(h, adminUsername, adminPassword)
	c := sessionCookie(rec)
	serve(h, withCookie(formRequest("/admin/logout", nil, true), c))

	lines := auditLines(t, logPath)
	if len(lines) != 3 {
		t.Fatalf("audit lines = %d, want 3:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	wants := []string{
		"192.0.2.1 POST /admin/login 422 login failed admin@example.com",
		"192.0.2.1 POST /admin/login 204 login ok admin@example.com",
		"192.0.2.1 POST /admin/logout 204",
	}
	for i, want := range wants {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want it to contain %q", i, lines[i], want)
		}
	}
}

func TestAuditLogRecordsRefusedMutation(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "audit.log")
	audit, err := auditlog.Open(logPath)
	if err != nil {
		t.Fatalf("auditlog.Open: %v", err)
	}
	srv, sqlDB := newServer(t, audit)
	h := srv.Handler()
	seedAdmin(t, sqlDB)

	serve(h, formRequest("/admin/login", url.Values{"username": {adminUsername}}, false))

	lines := auditLines(t, logPath)
	if len(lines) != 1 || !strings.Contains(lines[0], "POST /admin/login 403") {
		t.Errorf("audit lines = %q, want one line for the refused POST", lines)
	}
}
