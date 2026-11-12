package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// maxBodyBytes is the largest request body the server reads (DESIGN.md section 2.2).
const maxBodyBytes = 64 << 10

// securityHeaders sets the headers that every response carries.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}

// limitBody caps the size of request bodies.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// requireHTMX refuses a mutating request (POST or DELETE) unless it carries the
// HX-Request header. HTMX sends it, and a cross-site form cannot set it, so this
// blocks cross-site form submission (DESIGN.md section 4.2).
func requireHTMX(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutating := r.Method == http.MethodPost || r.Method == http.MethodDelete
		if mutating && !isHTMX(r) {
			http.Error(w, "forbidden: admin changes must come from the admin pages", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// auditDetailKey is the context key for the detail text of an audit line.
type auditDetailKey struct{}

// auditDetail carries the detail text from a handler to the audit middleware.
type auditDetail struct{ text string }

// setAuditDetail sets the detail written to the audit line for this request.
// It does nothing when the request is not audited.
func setAuditDetail(r *http.Request, text string) {
	if d, ok := r.Context().Value(auditDetailKey{}).(*auditDetail); ok {
		d.text = text
	}
}

// auditLog writes one line for each mutating request under /admin, with its
// final status and any detail (DESIGN.md section 4.4). A write error is printed
// to stderr and the request is still served.
func (s *Server) auditLog(next http.Handler) http.Handler {
	if s.audit == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAuditable(r) {
			next.ServeHTTP(w, r)
			return
		}
		detail := &auditDetail{}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), auditDetailKey{}, detail)))

		if err := s.audit.Record(s.now(), clientIP(r), r.Method, r.URL.EscapedPath(), rec.status, detail.text); err != nil {
			fmt.Fprintf(os.Stderr, "noticeboard: audit log: %v\n", err)
		}
	})
}

// isAuditable reports whether a request is a mutating request under /admin.
func isAuditable(r *http.Request) bool {
	mutating := r.Method == http.MethodPost || r.Method == http.MethodDelete
	return mutating && (r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/"))
}

// clientIP returns the host part of the remote address. Proxy headers are not trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// statusRecorder records the status code a handler writes.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(b)
}
