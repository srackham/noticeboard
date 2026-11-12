package web

import (
	"errors"
	"net/http"
	"time"

	"noticeboard/internal/auth"
)

// sessionCookieName is the name of the admin session cookie (DESIGN.md section 4.2).
const sessionCookieName = "noticeboard_session"

// wrongCredentials is the message for a wrong username or password. The same text is used for both, so the response does not reveal which one was wrong.
const wrongCredentials = "Username or password is incorrect."

// loginView is the data for the login page and its form fragment.
type loginView struct {
	Username string // as typed, so the user can correct a typo
	Error    string
	NoAdmin  bool // no administrator exists yet, so show the command-line hint
}

// handleLoginPage serves the login form. A user who is already logged in is sent to the dashboard.
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	ok, err := s.hasSession(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if ok {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	view, err := s.newLoginView(r, "", "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.execute(w, r, "admin-login", view, http.StatusOK)
}

// handleLogin checks the credentials. On success it starts a session and sets the cookie. On failure it returns the form with 422.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		formError(w, err)
		return
	}
	typed := r.PostForm.Get("username")
	username := auth.NormaliseUsername(typed)
	ctx := r.Context()

	ok, err := auth.Authenticate(ctx, s.db, username, r.PostForm.Get("password"))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !ok {
		setAuditDetail(r, "login failed "+username)
		view, err := s.newLoginView(r, typed, wrongCredentials)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.execute(w, r, "login-form", view, http.StatusUnprocessableEntity)
		return
	}

	setAuditDetail(r, "login ok "+username)
	now := s.now()
	if err := auth.DeleteExpiredSessions(ctx, s.db, now); err != nil {
		s.serverError(w, r, err)
		return
	}
	token, err := auth.CreateSession(ctx, s.db, now)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.SetCookie(w, newSessionCookie(r, token, int(auth.SessionLifetime/time.Second)))
	w.Header().Set("HX-Redirect", "/admin")
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout ends the session and clears the cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := auth.DeleteSession(r.Context(), s.db, sessionToken(r)); err != nil {
		s.serverError(w, r, err)
		return
	}
	// MaxAge -1 makes net/http write "Max-Age=0", which deletes the cookie.
	http.SetCookie(w, newSessionCookie(r, "", -1))
	w.Header().Set("HX-Redirect", "/admin/login")
	w.WriteHeader(http.StatusNoContent)
}

// handleAdmin serves the dashboard: the clock list, the clock form region and the settings form.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	s.renderDashboard(w, r)
}

// requireSession lets the request through only with a valid, unexpired session.
// Otherwise it redirects to the login page. An HTMX request gets 401 with
// HX-Redirect, so the browser navigates to the login page instead of swapping it into the page.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, err := s.hasSession(r)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		if !ok {
			redirectToLogin(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hasSession reports whether the request carries a valid session cookie.
func (s *Server) hasSession(r *http.Request) (bool, error) {
	return auth.ValidSession(r.Context(), s.db, sessionToken(r), s.now())
}

// newLoginView builds the login view. It reports whether an administrator account exists.
func (s *Server) newLoginView(r *http.Request, username, msg string) (loginView, error) {
	exists, err := auth.AdminExists(r.Context(), s.db)
	if err != nil {
		return loginView{}, err
	}
	return loginView{Username: username, Error: msg, NoAdmin: !exists}, nil
}

// sessionToken returns the session cookie value, or "" if there is none.
func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// newSessionCookie builds the session cookie. Secure is set only when the request arrived over TLS.
func newSessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/admin",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
	}
}

// redirectToLogin sends the browser to the login page, using HX-Redirect for HTMX requests.
func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/admin/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// isHTMX reports whether the request came from HTMX.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// formError answers a form that could not be read. An oversized body gets 413, anything else 400.
func formError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "bad request", http.StatusBadRequest)
}
