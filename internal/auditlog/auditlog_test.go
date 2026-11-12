package auditlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readLines returns the lines of the log file at path.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func newLog(t *testing.T) (*Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.log")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return l, path
}

func TestRecordFormat(t *testing.T) {
	l, path := newLog(t)
	// The record is written in UTC, whatever the local zone of the time.
	when := time.Date(2024, time.June, 1, 14, 5, 0, 0, time.FixedZone("UTC+5", 5*3600))

	if err := l.Record(when, "192.0.2.1", "POST", "/admin/login", 422, "login failed a@example.com"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := l.Record(when, "192.0.2.1", "POST", "/admin/logout", 204, ""); err != nil {
		t.Fatalf("Record: %v", err)
	}

	lines := readLines(t, path)
	want := []string{
		"2024-06-01T09:05:00Z 192.0.2.1 POST /admin/login 422 login failed a@example.com",
		"2024-06-01T09:05:00Z 192.0.2.1 POST /admin/logout 204",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestRecordSanitisesControlCharacters(t *testing.T) {
	l, path := newLog(t)
	when := time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)

	if err := l.Record(when, "192.0.2.1", "POST", "/admin/login", 422, "login failed a\r\n2024 forged"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("control characters split the record into %d lines: %q", len(lines), lines)
	}
}

func TestCapKeepsNewestLines(t *testing.T) {
	l, path := newLog(t)
	when := time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)

	total := MaxLines + 5
	for i := 0; i < total; i++ {
		if err := l.Record(when, "192.0.2.1", "POST", "/admin/zones", 200, fmt.Sprintf("line %d", i)); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	lines := readLines(t, path)
	if len(lines) != MaxLines {
		t.Fatalf("file has %d lines, want %d", len(lines), MaxLines)
	}
	if !strings.HasSuffix(lines[0], "line 5") {
		t.Errorf("oldest kept line = %q, want the one ending \"line 5\"", lines[0])
	}
	if !strings.HasSuffix(lines[len(lines)-1], fmt.Sprintf("line %d", total-1)) {
		t.Errorf("newest line = %q, want the last one written", lines[len(lines)-1])
	}
}

func TestReopenCountsExistingLines(t *testing.T) {
	when := time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "audit.log")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := 0; i < 600; i++ {
		if err := first.Record(when, "192.0.2.1", "GET", "/", 200, ""); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	for i := 0; i < 600; i++ {
		if err := second.Record(when, "192.0.2.1", "GET", "/", 200, ""); err != nil {
			t.Fatalf("Record after reopen: %v", err)
		}
	}
	if n := len(readLines(t, path)); n != MaxLines {
		t.Errorf("after reopen the file has %d lines, want %d", n, MaxLines)
	}
}

func TestOpenFailsForUnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "audit.log")
	if _, err := Open(path); err == nil {
		t.Error("Open succeeded for a path whose directory does not exist")
	}
}
