// Package auditlog appends one line per login attempt and per mutating admin
// request to a text file. The file keeps only the newest MaxLines lines
// (DESIGN.md section 4.4).
package auditlog

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// MaxLines is the most lines the file holds. The oldest lines are removed first.
const MaxLines = 1000

// Logger writes to one audit file. It is safe for concurrent use.
type Logger struct {
	mu    sync.Mutex
	path  string
	lines int // lines currently in the file
}

// Open prepares the audit file at path, creating it if needed, and counts its existing lines.
func Open(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	return &Logger{path: path, lines: bytes.Count(data, []byte{'\n'})}, nil
}

// Record appends one line in the form
//
//	<RFC 3339 UTC time> <client IP> <METHOD> <path> <status> [detail]
//
// Control characters in the fields are replaced by '_', so a field cannot
// start a new line.
func (l *Logger) Record(now time.Time, ip, method, path string, status int, detail string) error {
	fields := []string{
		now.UTC().Format(time.RFC3339),
		clean(ip),
		clean(method),
		clean(path),
		strconv.Itoa(status),
	}
	if d := strings.TrimRight(detail, " "); d != "" {
		fields = append(fields, clean(d))
	}
	line := strings.Join(fields, " ")

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := appendLine(l.path, line); err != nil {
		return err
	}
	l.lines++
	if l.lines > MaxLines {
		return l.trim()
	}
	return nil
}

// trim keeps the newest MaxLines lines. It writes a temporary file and renames
// it over the log, so a crash leaves the old file intact. The caller must hold l.mu.
func (l *Logger) trim() error {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return fmt.Errorf("trim audit log: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) > MaxLines {
		lines = lines[len(lines)-MaxLines:]
	}
	out := []byte(strings.Join(lines, "\n") + "\n")

	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("trim audit log: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return fmt.Errorf("trim audit log: %w", err)
	}
	l.lines = len(lines)
	return nil
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		f.Close()
		return fmt.Errorf("write audit log: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

// clean replaces control characters with '_'.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '_'
		}
		return r
	}, s)
}
