//go:build !android

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// InstallUpdate downloads and installs the latest release, then restarts the app.
// It returns an error message, or "" when the restart is underway.
func (a *App) InstallUpdate() string {
	a.mu.Lock()
	info := a.lastUpdate
	a.mu.Unlock()
	if info.assetURL == "" {
		var err error
		if info, err = fetchLatestRelease(); err != nil {
			return err.Error()
		}
	}
	if !info.Available {
		return "You're already on the latest version."
	}
	if manualUpdate {
		// macOS: download the new .app in the browser; it replaces the old one in Applications
		wruntime.BrowserOpenURL(a.ctx, info.assetURL)
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return err.Error()
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)
	newPath := exe + ".new"

	progress := func(done, total int64) {
		if total > 0 {
			a.emitUI("updateProgress", map[string]any{"pct": int(done * 100 / total)})
		}
	}
	sum, err := download(info.assetURL, newPath, info.Size, progress)
	if err != nil {
		os.Remove(newPath)
		if os.IsPermission(err) {
			return "Can't write to " + dir + " — move IRChatterbox to a folder you own (e.g. your Documents) or download the update manually."
		}
		return "Download failed: " + err.Error()
	}
	if info.sumsURL != "" {
		want, err := expectedSum(info.sumsURL, updateAsset)
		if err != nil {
			os.Remove(newPath)
			return "Couldn't verify the download: " + err.Error()
		}
		if want != "" && !strings.EqualFold(want, sum) {
			os.Remove(newPath)
			return "The download didn't match its checksum, so it wasn't installed. Try again later."
		}
	}
	os.Chmod(newPath, 0o755) // Linux/macOS: keep it executable
	if msg := a.swapAndRestart(exe, newPath); msg != "" {
		return msg
	}
	return ""
}

// swapAndRestart replaces exe with newPath (Windows lets you rename a running exe), starts
// the new version once this one exits, then quits. Returns an error message or "".
func (a *App) swapAndRestart(exe, newPath string) string {
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return "Couldn't replace the running app: " + err.Error()
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(old, exe) // put it back
		return "Couldn't install the update: " + err.Error()
	}
	cmd := exec.Command(exe, "--wait-pid", strconv.Itoa(os.Getpid()), "--updated")
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return "Updated, but couldn't restart automatically — please start IRChatterbox again."
	}
	a.saveWindow()
	go func() {
		time.Sleep(300 * time.Millisecond)
		a.mu.Lock()
		a.allowQuit = true
		a.mu.Unlock()
		wruntime.Quit(a.ctx)
	}()
	return ""
}

// ---------- local builds: IRChatterbox.exe.new next to the exe ----------
//
// Dropping a new build next to the app as "IRChatterbox.exe.new" updates it in place, so the
// exe path never changes (taskbar pins and shortcuts keep working):
//   - at launch, a waiting .new is swapped in before the app starts
//   - while running, the app notices it and offers "Restart to update"

func pendingBuild() (exe, newPath string, ok bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", false
	}
	exe, _ = filepath.EvalSymlinks(exe)
	newPath = exe + ".new"
	st, err := os.Stat(newPath)
	if err != nil || st.Size() < 1<<20 || time.Since(st.ModTime()) < 3*time.Second {
		return exe, newPath, false // missing, too small, or still being copied
	}
	return exe, newPath, true
}

// applyPendingBuildAtLaunch runs before the window opens. If a .new build is waiting it is
// swapped in and started, and this process exits.
func applyPendingBuildAtLaunch() {
	exe, newPath, ok := pendingBuild()
	if !ok {
		return
	}
	old := exe + ".old"
	os.Remove(old)
	if os.Rename(exe, old) != nil {
		return
	}
	if os.Rename(newPath, exe) != nil {
		os.Rename(old, exe)
		return
	}
	args := append([]string{"--wait-pid", strconv.Itoa(os.Getpid())}, os.Args[1:]...)
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	if cmd.Start() == nil {
		os.Exit(0)
	}
}

// watchPendingBuild tells the UI when a .new build appears while the app is running.
func (a *App) watchPendingBuild() {
	announced := false
	for range time.Tick(5 * time.Second) {
		_, _, ok := pendingBuild()
		if ok && !announced {
			a.emitUI("localUpdateReady", map[string]any{})
		}
		announced = ok
	}
}

// ApplyLocalUpdate swaps in IRChatterbox.exe.new and restarts.
func (a *App) ApplyLocalUpdate() string {
	exe, newPath, ok := pendingBuild()
	if !ok {
		return "The new build isn't there any more."
	}
	return a.swapAndRestart(exe, newPath)
}

// cleanupOldExe removes the previous version left behind by an update.
func cleanupOldExe() {
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
		os.Remove(exe + ".new")
	}
}
