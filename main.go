// Command noticeboard serves world clocks on a public board and provides an
// administration dashboard for managing them.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // embeds the IANA zone database, so zones load without system tzdata (Windows)

	"golang.org/x/term"

	"noticeboard/internal/auditlog"
	"noticeboard/internal/auth"
	"noticeboard/internal/db"
	"noticeboard/internal/web"
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
  noticeboard admin init <username> [--db FILE]
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
  noticeboard admin init <username> [--db FILE]

Create or replace the administrator account. The password is prompted for
without echo; it is never taken from the command line.

Options:
  --db FILE    SQLite database file (default ./noticeboard.db)
`

const adminInitUsage = `Usage:
  noticeboard admin init <username> [--db FILE]

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

	if err := auth.DeleteExpiredSessions(context.Background(), sqlDB, time.Now()); err != nil {
		fmt.Fprintf(stderr, "noticeboard: %v\n", err)
		return 1
	}
	audit, err := auditlog.Open(*logPath)
	if err != nil {
		fmt.Fprintf(stderr, "noticeboard: %v\n", err)
		return 1
	}

	if err := serveHTTP(*addr, sqlDB, audit, stdout); err != nil {
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
	case "init":
		return runAdminInit(args[1:], stdout, stderr)
	default:
		return usageFail(stderr, adminUsage, fmt.Sprintf("unknown admin subcommand %q", args[0]))
	}
}

func runAdminInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("admin init", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDB, "SQLite database file")

	positional, code, ok := parseCommand(fs, adminInitUsage, args, stdout, stderr)
	if !ok {
		return code
	}
	switch {
	case len(positional) == 0:
		return usageFail(stderr, adminInitUsage, "missing <username>")
	case len(positional) > 1:
		return usageFail(stderr, adminInitUsage, fmt.Sprintf("unexpected argument %q", positional[1]))
	}

	prompt := func(p string) (string, error) { return promptPassword(stderr, p) }
	if err := adminInit(*dbPath, positional[0], prompt); err != nil {
		fmt.Fprintf(stderr, "noticeboard: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "noticeboard: administrator account saved for %s; existing sessions ended\n",
		auth.NormaliseUsername(positional[0]))
	return 0
}

// promptFunc reads a line of secret input, for example without echo.
type promptFunc func(prompt string) (string, error)

// adminInit creates or replaces the administrator account. The password is
// entered twice. If the entries differ, nothing is changed. The database is
// opened only after the password is confirmed.
func adminInit(dbPath, username string, prompt promptFunc) error {
	// Check the username before the password is asked for.
	if err := auth.ValidateUsername(auth.NormaliseUsername(username)); err != nil {
		return err
	}
	password, err := prompt("Password: ")
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	again, err := prompt("Confirm password: ")
	if err != nil {
		return err
	}
	if password != again {
		return errors.New("passwords do not match; no changes made")
	}

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return auth.UpsertAdmin(context.Background(), sqlDB, username, password)
}

// promptPassword reads one line from the terminal without echo. It refuses to
// run when stdin is not a terminal, so a password is never read from a pipe or
// a file.
func promptPassword(stderr io.Writer, prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("the password must be entered at an interactive terminal")
	}
	fmt.Fprint(stderr, prompt)
	secret, err := term.ReadPassword(fd)
	fmt.Fprintln(stderr)
	if err != nil {
		return "", err
	}
	return string(secret), nil
}

// serveHTTP listens on addr and serves until SIGINT or SIGTERM, then shuts
// down gracefully.
func serveHTTP(addr string, sqlDB *sql.DB, audit *auditlog.Logger, stdout io.Writer) error {
	site, err := web.New(sqlDB, assetFS, audit)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           site.Handler(),
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
