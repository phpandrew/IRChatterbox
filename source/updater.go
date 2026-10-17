package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Auto-update from GitHub Releases.
//
// The newest non-pre-release release is compared with appVersion. Installing downloads
// IRChatterbox.exe from that release, checks it against SHA256SUMS.txt, swaps it in for
// the running exe (Windows allows renaming a running exe), starts the new one and quits.

const updateRepo = "phpandrew/IRChatterbox"

type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Notes     string `json:"notes"`
	URL       string `json:"url"`  // release page
	Size      int64  `json:"size"` // exe size in bytes
	Error     string `json:"error,omitempty"`

	exeURL  string
	sumsURL string
}

type ghRelease struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

var updateClient = &http.Client{Timeout: 20 * time.Second}

func fetchLatestRelease() (UpdateInfo, error) {
	info := UpdateInfo{Current: appVersion}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "IRChatterbox/"+appVersion)
	resp, err := updateClient.Do(req)
	if err != nil {
		return info, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return info, errors.New("no releases published yet")
	}
	if resp.StatusCode != 200 {
		return info, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return info, err
	}
	info.Latest = strings.TrimPrefix(rel.TagName, "v")
	info.Notes = rel.Body
	info.URL = rel.HTMLURL
	for _, a := range rel.Assets {
		switch a.Name {
		case "IRChatterbox.exe":
			info.exeURL, info.Size = a.URL, a.Size
		case "SHA256SUMS.txt":
			info.sumsURL = a.URL
		}
	}
	info.Available = info.exeURL != "" && compareVersions(info.Latest, appVersion) > 0
	return info, nil
}

// compareVersions compares versions: 1.2.10 > 1.2.9; 0.3.0-beta1 < 0.3.0 < 0.3.0-dev.4
// (dev builds are commits made after the 0.3.0 tag, so they're newer than it).
func compareVersions(a, b string) int {
	split := func(v string) ([]int, string) {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexByte(v, '+'); i >= 0 {
			v = v[:i]
		}
		pre := ""
		if i := strings.IndexByte(v, '-'); i >= 0 {
			v, pre = v[:i], v[i+1:]
		}
		var nums []int
		for _, p := range strings.Split(v, ".") {
			n, _ := strconv.Atoi(p)
			nums = append(nums, n)
		}
		for len(nums) < 3 {
			nums = append(nums, 0)
		}
		return nums, pre
	}
	an, ap := split(a)
	bn, bp := split(b)
	for i := 0; i < len(an) && i < len(bn); i++ {
		if an[i] != bn[i] {
			if an[i] > bn[i] {
				return 1
			}
			return -1
		}
	}
	// suffix rank: beta/rc (before the release) < release < dev builds (built from commits
	// *after* that release's tag, e.g. 0.2.5-dev.3 = 3 commits past v0.2.5)
	rank := func(p string) int {
		switch {
		case p == "":
			return 1
		case strings.HasPrefix(p, "dev"):
			return 2
		default:
			return 0
		}
	}
	if ra, rb := rank(ap), rank(bp); ra != rb {
		if ra > rb {
			return 1
		}
		return -1
	}
	// dev.10 vs dev.9: compare the commit counts as numbers
	if strings.HasPrefix(ap, "dev.") && strings.HasPrefix(bp, "dev.") {
		na, _ := strconv.Atoi(strings.TrimPrefix(ap, "dev."))
		nb, _ := strconv.Atoi(strings.TrimPrefix(bp, "dev."))
		if na != nb {
			if na > nb {
				return 1
			}
			return -1
		}
		return 0
	}
	switch {
	case ap == bp:
		return 0
	case ap > bp:
		return 1
	default:
		return -1
	}
}

// CheckForUpdate is called by the About tab ("Check for updates") and the startup check.
func (a *App) CheckForUpdate() UpdateInfo {
	info, err := fetchLatestRelease()
	if err != nil {
		info.Error = err.Error()
	}
	a.mu.Lock()
	a.lastUpdate = info
	a.mu.Unlock()
	return info
}

// SkipUpdateVersion stops reminding about a particular version.
func (a *App) SkipUpdateVersion(v string) {
	a.store.Update(func(c *Config) { c.Settings.SkippedVersion = v })
}

// autoUpdateLoop checks shortly after launch and then every 6 hours.
func (a *App) autoUpdateLoop() {
	time.Sleep(15 * time.Second)
	for {
		set := a.store.Get().Settings
		if set.AutoUpdateCheck {
			info := a.CheckForUpdate()
			if info.Available && info.Latest != set.SkippedVersion && a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "updateAvailable", info)
			}
		}
		time.Sleep(6 * time.Hour)
	}
}

// InstallUpdate downloads and installs the latest release, then restarts the app.
// It returns an error message, or "" when the restart is underway.
func (a *App) InstallUpdate() string {
	a.mu.Lock()
	info := a.lastUpdate
	a.mu.Unlock()
	if info.exeURL == "" {
		var err error
		if info, err = fetchLatestRelease(); err != nil {
			return err.Error()
		}
	}
	if !info.Available {
		return "You're already on the latest version."
	}
	exe, err := os.Executable()
	if err != nil {
		return err.Error()
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)
	newPath := exe + ".new"

	progress := func(done, total int64) {
		if a.ctx != nil && total > 0 {
			wruntime.EventsEmit(a.ctx, "updateProgress", map[string]any{"pct": int(done * 100 / total)})
		}
	}
	sum, err := download(info.exeURL, newPath, info.Size, progress)
	if err != nil {
		os.Remove(newPath)
		if os.IsPermission(err) {
			return "Can't write to " + dir + " — move IRChatterbox to a folder you own (e.g. your Documents) or download the update manually."
		}
		return "Download failed: " + err.Error()
	}
	if info.sumsURL != "" {
		want, err := expectedSum(info.sumsURL, "IRChatterbox.exe")
		if err != nil {
			os.Remove(newPath)
			return "Couldn't verify the download: " + err.Error()
		}
		if want != "" && !strings.EqualFold(want, sum) {
			os.Remove(newPath)
			return "The download didn't match its checksum, so it wasn't installed. Try again later."
		}
	}
	// swap: running exe → .old, new → exe
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(newPath)
		return "Couldn't replace the running app: " + err.Error()
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(old, exe) // put it back
		return "Couldn't install the update: " + err.Error()
	}
	// start the new version once this one has exited, then quit
	cmd := exec.Command(exe, "--wait-pid", strconv.Itoa(os.Getpid()), "--updated")
	cmd.Dir = dir
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

func download(url, dest string, size int64, progress func(done, total int64)) (string, error) {
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %s", resp.Status)
	}
	if resp.ContentLength > 0 {
		size = resp.ContentLength
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	var done int64
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return "", err
			}
			h.Write(buf[:n])
			done += int64(n)
			if time.Since(last) > 150*time.Millisecond {
				progress(done, size)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	progress(size, size)
	return hex.EncodeToString(h.Sum(nil)), f.Sync()
}

func expectedSum(url, name string) (string, error) {
	resp, err := updateClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", nil
}

// cleanupOldExe removes the previous version left behind by an update.
func cleanupOldExe() {
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
		os.Remove(exe + ".new")
	}
}
