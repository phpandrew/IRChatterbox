//go:build !android

package core

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// A panic is written to crash-current.log; the next start turns it into a crash report.
func TestCrashCapture(t *testing.T) {
	if dir := os.Getenv("CRASH_HELPER"); dir != "" {
		os.Setenv("XDG_CONFIG_HOME", dir)
		os.Setenv("HOME", dir)
		a := NewApp()
		a.initLogs()
		go func() { panic("boom from a goroutine") }()
		select {}
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestCrashCapture")
	cmd.Env = append(os.Environ(), "CRASH_HELPER="+dir)
	if err := cmd.Run(); err == nil {
		t.Fatal("helper should have crashed")
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	os.Setenv("HOME", dir)
	a := NewApp()
	a.initLogs()
	d := a.GetDiagnostics()
	if d.LastCrash == "" || !strings.Contains(d.CrashText, "boom from a goroutine") {
		t.Fatalf("crash not captured: %+v", d)
	}
	t.Log(d.LastCrash)
	if b, _ := os.ReadFile(d.LogDir + "/app.log"); !strings.Contains(string(b), "previous run crashed") {
		t.Fatalf("app.log missing crash note: %s", b)
	}
}
