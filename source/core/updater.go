package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	URL       string `json:"url"`    // release page
	Size      int64  `json:"size"`   // exe size in bytes
	Manual    bool   `json:"manual"` // the download opens in the browser instead of installing in place (macOS, Android)
	Error     string `json:"error,omitempty"`

	assetURL string // this platform's download (IRChatterbox.exe / IRChatterbox.apk)
	sumsURL  string
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

var updateClient = &http.Client{Timeout: 20 * time.Second, CheckRedirect: httpsOnlyRedirect}

// httpsOnlyRedirect lets a download follow GitHub's redirect to its file store, but never to
// a plain-http address.
func httpsOnlyRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != "https" {
		return errors.New("redirected to an insecure address")
	}
	return nil
}

// releaseURLOK: an asset address on GitHub's release hosts, over https.
func releaseURLOK(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	}
	return false
}

// verifyDownload checks a downloaded release file against the release's SHA256SUMS.txt.
// Without a checksum to compare with, nothing is installed.
func verifyDownload(info UpdateInfo, sum string) string {
	if info.sumsURL == "" {
		return "This release has no checksum file, so the download wasn't installed. You can download it from the release page instead."
	}
	want, err := expectedSum(info.sumsURL, updateAsset)
	if err != nil {
		return "Couldn't verify the download: " + err.Error()
	}
	if want == "" {
		return "The release's checksum file doesn't list this download, so it wasn't installed."
	}
	if !strings.EqualFold(want, sum) {
		return "The download didn't match its checksum, so it wasn't installed. Try again later."
	}
	return ""
}

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
		if !releaseURLOK(a.URL) {
			continue // downloads come from GitHub over https, nowhere else
		}
		switch a.Name {
		case updateAsset:
			info.assetURL, info.Size = a.URL, a.Size
		case "SHA256SUMS.txt":
			info.sumsURL = a.URL
		}
	}
	info.Available = info.assetURL != "" && compareVersions(info.Latest, appVersion) > 0
	info.Manual = manualUpdate
	return info, nil
}

// compareVersions compares versions: 1.2.10 > 1.2.9; 0.3.0-dev.4 < 0.3.0-dev.10 < 0.3.0.
// Dev builds are working towards the version in VERSION, so they come before its release.
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
	// a pre-release (0.3.0-dev.4, 0.3.0-beta1) comes before its release (0.3.0)
	if (ap == "") != (bp == "") {
		if ap == "" {
			return 1
		}
		return -1
	}
	// both pre-releases: compare dot-separated parts, numbers as numbers (dev.10 > dev.9)
	pa, pb := strings.Split(ap, "."), strings.Split(bp, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea == nil && eb == nil {
			if na > nb {
				return 1
			}
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
		return -1
	}
	switch {
	case len(pa) > len(pb):
		return 1
	case len(pa) < len(pb):
		return -1
	}
	return 0
}

// storeName is set when this build is installed from an app store ("play" for Google Play).
// The store delivers updates, so the GitHub update check and download are off.
var storeName string

// SetStore marks this build as installed from a store. Must be called before NewApp.
func SetStore(name string) { storeName = name }

// GetStore is "" for builds from GitHub Releases, or the store the app came from.
func (a *App) GetStore() string { return storeName }

// CheckForUpdate is called by the About tab ("Check for updates") and the startup check.
func (a *App) CheckForUpdate() UpdateInfo {
	if storeName != "" {
		return UpdateInfo{Current: appVersion, Latest: appVersion}
	}
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
	if storeName != "" {
		return
	}
	time.Sleep(15 * time.Second)
	for {
		set := a.store.Get().Settings
		if set.AutoUpdateCheck {
			info := a.CheckForUpdate()
			if info.Available && info.Latest != set.SkippedVersion {
				a.emitUI("updateAvailable", info)
			}
		}
		time.Sleep(6 * time.Hour)
	}
}
