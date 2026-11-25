package main

// Desktop mode (DESIGN.md section 9): serve --launch and open <address> open the
// board in a browser app-mode window. The launcher is adapted from htmx-todos/gui.go.
// It is plain Go, so the binary needs no webview library and no CGO.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// browserCommand is a program and its arguments.
type browserCommand struct {
	name string
	args []string
}

// openWindow opens url in an app-mode window. It does not wait for the browser,
// and it releases the process, so closing the window does not stop the server.
// When no browser is found, or the browser cannot start, the URL is printed to
// stdout and the server carries on.
func openWindow(url string, stdout, stderr io.Writer) {
	bc, ok := pickBrowser(runtime.GOOS, url)
	if !ok {
		fmt.Fprintf(stdout, "noticeboard: no browser found; open %s\n", url)
		return
	}

	cmd := exec.Command(bc.name, bc.args...)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "noticeboard: could not start a browser: %v\n", err)
		fmt.Fprintf(stdout, "noticeboard: open %s\n", url)
		return
	}
	// Do not track the browser: the server runs until it is stopped.
	_ = cmd.Process.Release()
}

// pickBrowser chooses the browser and arguments for goos, in the order DESIGN.md
// section 9 gives. It reports false when none is found.
func pickBrowser(goos, url string) (browserCommand, bool) {
	switch goos {
	case "windows":
		if p, ok := firstFound(windowsBrowsers()...); ok {
			return browserCommand{name: p, args: chromiumArgs(url)}, true
		}
	case "darwin":
		const chrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if fileExists(chrome) {
			return browserCommand{name: chrome, args: chromiumArgs(url)}, true
		}
		// open is always present on macOS, so it is the last resort.
		return browserCommand{name: "open", args: []string{url}}, true
	case "linux":
		if p, ok := firstFound("google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "brave-browser", "brave"); ok {
			return browserCommand{name: p, args: chromiumArgs(url)}, true
		}
		// xdg-open cannot guarantee app mode, so the window may be a normal tab.
		if p, ok := firstFound("xdg-open"); ok {
			return browserCommand{name: p, args: []string{url}}, true
		}
	}
	return browserCommand{}, false
}

// windowsBrowsers lists the Edge and Chrome programs to try on Windows: first on
// PATH, then at their standard install locations.
func windowsBrowsers() []string {
	var candidates []string
	candidates = append(candidates, "msedge")
	if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
		candidates = append(candidates, filepath.Join(pf, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	candidates = append(candidates, "chrome")
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidates = append(candidates, filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"))
	}
	return candidates
}

// firstFound returns the first candidate that exists. An absolute path is
// checked directly. Any other name is looked up on PATH.
func firstFound(candidates ...string) (string, bool) {
	for _, c := range candidates {
		if filepath.IsAbs(c) {
			if fileExists(c) {
				return c, true
			}
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, true
		}
	}
	return "", false
}

// chromiumArgs returns the arguments for a Chromium-family browser: app mode, and a
// dedicated profile. The profile makes the browser start a new instance, so the
// window opens even when that browser is already running with its own profile.
func chromiumArgs(url string) []string {
	return []string{
		"--app=" + url,
		"--user-data-dir=" + filepath.Join(os.TempDir(), "noticeboard-gui"),
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// openURL returns the browser URL for address. A full URL is used as is; a
// listen address such as "127.0.0.1:8080" is converted with boardURL.
func openURL(address string) string {
	if strings.Contains(address, "://") {
		return address
	}
	return boardURL(address)
}

// boardURL returns the address of the board for a listen address such as
// "127.0.0.1:8080" or ":8080". An empty or unspecified host (0.0.0.0 or ::) is
// opened as the loopback address, because Windows browsers cannot open 0.0.0.0.
func boardURL(hostPort string) string {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return "http://" + hostPort + "/clocks"
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/clocks"
}

// isAddrInUse reports whether a listen failed because the address is already in use.
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	// Windows reports WSAEADDRINUSE (10048), which the syscall package does not
	// map to EADDRINUSE.
	var errno syscall.Errno
	return runtime.GOOS == "windows" && errors.As(err, &errno) && errno == 10048
}
