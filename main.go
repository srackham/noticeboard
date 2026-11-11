// Command noticeboard serves world clocks on a public board and provides an
// administration dashboard for managing them.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"noticeboard/internal/db"
)

const (
	defaultAddr = "127.0.0.1:8080"
	defaultDB   = "./noticeboard.db"
	defaultLog  = "./noticeboard.log"

	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

const topUsage = `Usage:
  noticeboard serve [--addr ADDR] [--db FILE] [--log FILE] [--gui]
  noticeboard admin upsert <email> [--db FILE]
  noticeboard --help

Commands:
  serve    Run the web server (public board and admin dashboard)
  admin    Administration commands

Run 'noticeboard <command> --help' for command options.
`

const serveUsage = `Usage:
  noticeboard serve [--addr ADDR] [--db FILE] [--log FILE] [--gui]

Run the web server.

Options:
  --addr ADDR  listen address (default 127.0.0.1:8080)
  --db FILE    SQLite database file (default ./noticeboard.db)
  --log FILE   audit log file (default ./noticeboard.log)
  --gui        open the board in an app-mode browser window
`

const adminUsage = `Usage:
  noticeboard admin upsert <email> [--db FILE]

Create or replace the administrator account. The password is prompted for
without echo; it is never taken from the command line.

Options:
  --db FILE    SQLite database file (default ./noticeboard.db)
`

const adminUpsertUsage = `Usage:
  noticeboard admin upsert <email> [--db FILE]

Create or replace the administrator account.

Options:
  --db FILE    SQLite database file (default ./noticeboard.db)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches the command line and returns the process exit status:
// 0 on success or help, 2 for usage errors, 1 for runtime failures.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, topUsage)
		return 2
	}
	switch args[0] {
	case "-h", "-help", "--help":
		fmt.Fprint(stdout, topUsage)
		return 0
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "admin":
		return runAdmin(args[1:], stdout, stderr)
	default:
		return usageFail(stderr, topUsage, fmt.Sprintf("unknown command %q", args[0]))
	}
}

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", defaultAddr, "listen address")
	dbPath := fs.String("db", defaultDB, "SQLite database file")
	logPath := fs.String("log", defaultLog, "audit log file")
	gui := fs.Bool("gui", false, "open the board in an app-mode browser window")

	positional, code, ok := parseCommand(fs, serveUsage, args, stdout, stderr)
	if !ok {
		return code
	}
	if len(positional) > 0 {
		return usageFail(stderr, serveUsage, fmt.Sprintf("unexpected argument %q", positional[0]))
	}

	// The audit log is wired in step 6.
	_ = logPath
	if *gui {
		fmt.Fprintln(stderr, "noticeboard: --gui is not implemented yet")
		return 1
	}
	sqlDB, err := db.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(stderr, "noticeboard: %v\n", err)
		return 1
	}
	defer sqlDB.Close()

	if err := serveHTTP(*addr, stdout); err != nil {
		fmt.Fprintf(stderr, "noticeboard: %v\n", err)
		return 1
	}
	return 0
}

func runAdmin(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageFail(stderr, adminUsage, "missing admin subcommand")
	}
	switch args[0] {
	case "-h", "-help", "--help":
		fmt.Fprint(stdout, adminUsage)
		return 0
	case "upsert":
		return runAdminUpsert(args[1:], stdout, stderr)
	default:
		return usageFail(stderr, adminUsage, fmt.Sprintf("unknown admin subcommand %q", args[0]))
	}
}

func runAdminUpsert(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("admin upsert", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDB, "SQLite database file")

	positional, code, ok := parseCommand(fs, adminUpsertUsage, args, stdout, stderr)
	if !ok {
		return code
	}
	switch {
	case len(positional) == 0:
		return usageFail(stderr, adminUpsertUsage, "missing <email>")
	case len(positional) > 1:
		return usageFail(stderr, adminUpsertUsage, fmt.Sprintf("unexpected argument %q", positional[1]))
	}

	// Step 1 skeleton: account creation is implemented in step 6.
	_ = dbPath
	fmt.Fprintln(stderr, "noticeboard: admin upsert is not implemented yet")
	return 1
}

// serveHTTP listens on addr and serves until SIGINT or SIGTERM, then shuts
// down gracefully.
func serveHTTP(addr string, stdout io.Writer) error {
	templates, err := template.ParseFS(assetFS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}
	staticFS, err := fs.Sub(assetFS, "static")
	if err != nil {
		return fmt.Errorf("static files: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// Placeholder until the board is implemented in step 5.
		if err := templates.ExecuteTemplate(w, "index.html", nil); err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "noticeboard: listening on http://%s\n", ln.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		// Serve returns only on failure here, because Shutdown has not been called.
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// In-flight requests did not finish in time; close the remaining connections.
		srv.Close()
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// parseCommand parses flags and positional arguments in any order. On -h or
// --help it prints usage to stdout and reports exit status 0. On a usage error
// it prints the message and usage to stderr and reports exit status 2. When ok
// is true, the parsed positional arguments are returned.
func parseCommand(fs *flag.FlagSet, usage string, args []string, stdout, stderr io.Writer) (positional []string, code int, ok bool) {
	// The flag package's own messages are discarded; usageFail reports errors.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	positional, err := parseInterspersed(fs, args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		return nil, 0, false
	case err != nil:
		return nil, usageFail(stderr, usage, err.Error()), false
	}
	return positional, 0, true
}

// parseInterspersed lets flags appear after positional arguments, which the
// standard flag package does not do by itself. Arguments after "--" are
// always positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		consumed := len(args) - len(rest)
		if consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// usageFail prints an error message followed by usage to stderr and returns
// exit status 2.
func usageFail(stderr io.Writer, usage, msg string) int {
	fmt.Fprintf(stderr, "noticeboard: %s\n\n%s", msg, usage)
	return 2
}
