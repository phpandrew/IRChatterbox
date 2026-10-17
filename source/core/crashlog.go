//go:build !android

package core

// Logs for troubleshooting: app.log (recent errors and events, trimmed to ~1 MB) and crash
// reports. A crash (an unhandled panic or fatal error) is written by the Go runtime to
// crash-current.log; on the next start it's renamed crash-<time>.log and the app offers to
// report it.

import (
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

type Diagnostics struct {
	Version   string `json:"version"`
	OS        string `json:"os"`
	LogDir    string `json:"logDir"`
	LastCrash string `json:"lastCrash"` // path of a crash from the previous run, "" if none
	CrashText string `json:"crashText"` // its first lines
	Reported  bool   `json:"reported"`  // already offered once
}

func (a *App) diagDir() string { return filepath.Join(a.store.dir, "diagnostics") }

// initLogs sets up app.log and crash capture. Called once at startup by the desktop host.
func (a *App) initLogs() {
	dir := a.diagDir()
	os.MkdirAll(dir, 0o755)

	// app.log: keep one previous file when it passes 1 MB
	logPath := filepath.Join(dir, "app.log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > 1<<20 {
		os.Rename(logPath, logPath+".1")
	}
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
		log.SetFlags(log.LstdFlags)
	}
	log.Printf("IRChatterbox %s starting on %s/%s", appVersion, runtime.GOOS, runtime.GOARCH)

	// a crash last time left crash-current.log behind
	cur := filepath.Join(dir, "crash-current.log")
	if st, err := os.Stat(cur); err == nil && st.Size() > 0 {
		dst := filepath.Join(dir, "crash-"+st.ModTime().Format("2006-01-02_15-04-05")+".log")
		if os.Rename(cur, dst) == nil {
			a.lastCrash = dst
			log.Printf("previous run crashed, saved as %s", filepath.Base(dst))
		}
	}
	pruneCrashes(dir, 10)
	if f, err := os.Create(cur); err == nil {
		// written by the runtime on a fatal error; empty when the app exits normally
		debug.SetCrashOutput(f, debug.CrashOptions{})
	}
}

// pruneCrashes keeps the newest n crash reports.
func pruneCrashes(dir string, n int) {
	m, _ := filepath.Glob(filepath.Join(dir, "crash-2*.log"))
	sort.Strings(m)
	for len(m) > n {
		os.Remove(m[0])
		m = m[1:]
	}
}

// GetDiagnostics is for Settings → About and the "closed unexpectedly" notice.
func (a *App) GetDiagnostics() Diagnostics {
	d := Diagnostics{Version: appVersion, OS: runtime.GOOS + "/" + runtime.GOARCH, LogDir: a.diagDir(), LastCrash: a.lastCrash}
	if a.lastCrash != "" {
		if b, err := os.ReadFile(a.lastCrash); err == nil {
			d.CrashText = firstLines(string(b), 40, 4000)
		}
	}
	return d
}

// DismissCrash stops the notice from showing again this session.
func (a *App) DismissCrash() { a.lastCrash = "" }

// OpenDiagnostics shows the folder with app.log and crash reports.
func (a *App) OpenDiagnostics() {
	os.MkdirAll(a.diagDir(), 0o755)
	openFolder(a.diagDir())
}

// ReportProblem opens a new GitHub issue with the version, OS and (if there was one) the
// start of the crash report filled in. Nothing is sent until the user submits it.
func (a *App) ReportProblem() {
	d := a.GetDiagnostics()
	var b strings.Builder
	b.WriteString("**What happened?**\n\n\n**Steps to reproduce (if you know them)**\n\n\n")
	fmt.Fprintf(&b, "**Version:** %s  \n**OS:** %s\n\n", d.Version, d.OS)
	if d.CrashText != "" {
		b.WriteString("**Crash report** (first lines; please attach the whole file from Settings → About → Open logs)\n```\n")
		b.WriteString(firstLines(d.CrashText, 25, 1500))
		b.WriteString("\n```\n")
	} else {
		b.WriteString("If it's useful, attach `app.log` from Settings → About → Open logs (check it first: it can contain server names).\n")
	}
	title := "Problem report"
	if d.CrashText != "" {
		title = "Crash: " + firstLines(d.CrashText, 1, 80)
	}
	u := "https://github.com/" + updateRepo + "/issues/new?title=" + url.QueryEscape(title) + "&body=" + url.QueryEscape(b.String())
	a.openBrowser(u)
}

// LogUI records a frontend error (window.onerror / unhandled promise) in app.log.
func (a *App) LogUI(msg string) {
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	log.Printf("[ui] %s", msg)
}

func firstLines(s string, n, max int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(out) > max {
		out = out[:max]
	}
	return out
}
