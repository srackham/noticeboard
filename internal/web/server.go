// Package web holds the HTTP handlers and their HTML rendering.
package web

import (
	"database/sql"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"
)

// Server serves the public board. Admin routes are added in later steps.
type Server struct {
	db        *sql.DB
	templates *template.Template
	static    fs.FS
	now       func() time.Time // injected by tests
}

// New parses the templates in assets/templates and prepares the static files
// in assets/static. assets is the embedded asset tree in production.
func New(db *sql.DB, assets fs.FS) (*Server, error) {
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("static files: %w", err)
	}
	return &Server{db: db, templates: templates, static: static, now: time.Now}, nil
}

// Handler returns the routes for the public site.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /board", s.handleBoard)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(s.static)))
	return mux
}
