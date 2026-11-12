// Package web holds the HTTP handlers and their HTML rendering.
package web

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"time"

	"noticeboard/internal/auditlog"
)

// Server serves the public board and the administration pages.
type Server struct {
	db        *sql.DB
	templates *template.Template
	static    fs.FS
	audit     *auditlog.Logger // nil disables audit logging
	now       func() time.Time // injected by tests
}

// New parses the templates in assets/templates and prepares the static files
// in assets/static. assets is the embedded asset tree in production. audit may
// be nil.
func New(db *sql.DB, assets fs.FS, audit *auditlog.Logger) (*Server, error) {
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("static files: %w", err)
	}
	return &Server{db: db, templates: templates, static: static, audit: audit, now: time.Now}, nil
}

// Handler returns the routes, wrapped in the middleware that applies to all of them.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public site.
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /board", s.handleBoard)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(s.static)))

	// Administration. The login page and its POST are public. Logout and the
	// dashboard need a session. Every other /admin path needs one too.
	mux.Handle("GET /admin/login", http.HandlerFunc(s.handleLoginPage))
	mux.Handle("POST /admin/login", requireHTMX(http.HandlerFunc(s.handleLogin)))
	mux.Handle("POST /admin/logout", s.requireSession(requireHTMX(http.HandlerFunc(s.handleLogout))))
	mux.Handle("GET /admin", s.requireSession(http.HandlerFunc(s.handleAdmin)))

	// Clock management. Every change is a mutating request, so it also needs HX-Request.
	mux.Handle("GET /admin/zones/new", s.requireSession(http.HandlerFunc(s.handleZoneNew)))
	mux.Handle("POST /admin/zones", s.requireSession(requireHTMX(http.HandlerFunc(s.handleZoneCreate))))
	mux.Handle("GET /admin/zones/{id}/edit", s.requireSession(http.HandlerFunc(s.handleZoneEdit)))
	mux.Handle("POST /admin/zones/{id}", s.requireSession(requireHTMX(http.HandlerFunc(s.handleZoneUpdate))))
	mux.Handle("POST /admin/zones/{id}/toggle", s.requireSession(requireHTMX(http.HandlerFunc(s.handleZoneToggle))))
	mux.Handle("POST /admin/zones/{id}/move", s.requireSession(requireHTMX(http.HandlerFunc(s.handleZoneMove))))
	mux.Handle("DELETE /admin/zones/{id}", s.requireSession(requireHTMX(http.HandlerFunc(s.handleZoneDelete))))

	// Board settings.
	mux.Handle("POST /admin/settings", s.requireSession(requireHTMX(http.HandlerFunc(s.handleSettings))))

	mux.Handle("/admin/", s.requireSession(http.NotFoundHandler()))

	return securityHeaders(limitBody(s.auditLog(mux)))
}

// execute renders the named template into a buffer and writes it with the given
// status. Buffering means a template error gives a clean 500, not a partial page.
func (s *Server) execute(w http.ResponseWriter, r *http.Request, name string, data any, status int) {
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, data); err != nil {
		s.serverError(w, r, fmt.Errorf("render %s: %w", name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// serverError logs err and sends a generic 500 response.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
