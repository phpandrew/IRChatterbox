//go:build android

// Android-only part of the phone host (the shared part is host_mobile.go): updates.

package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const updateAsset = "IRChatterbox.apk"

// The keep-alive foreground service holds the connection while the app is in the background.
const keepAliveSupported = true

// InstallUpdate downloads the new APK inside the app, checks it against SHA256SUMS.txt and
// hands it to Android's installer ("Do you want to update this app?"). Android only accepts
// it if it's signed with the same key as the installed app. The Google Play build opens the
// store instead.
func (a *App) InstallUpdate() string {
	if storeName == "play" {
		a.openBrowser("https://play.google.com/store/apps/details?id=io.github.phpandrew.irchatterbox")
		return ""
	}
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
	if info.assetURL == "" || !strings.HasPrefix(info.assetURL, "https://") {
		return fmt.Sprintf("No Android download in release %s", info.Latest)
	}
	h := currentHost()
	if h == nil {
		return "The app isn't ready yet. Try again in a moment."
	}

	dir := updatesDir()
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err.Error()
	}
	apk := filepath.Join(dir, "IRChatterbox-"+safeName(info.Latest)+".apk")
	progress := func(done, total int64) {
		if total > 0 {
			a.emitUI("updateProgress", map[string]any{"pct": int(done * 100 / total)})
		}
	}
	sum, err := download(info.assetURL, apk, info.Size, progress)
	if err != nil {
		os.Remove(apk)
		return "Download failed: " + err.Error()
	}
	if msg := verifyDownload(info, sum); msg != "" {
		os.Remove(apk)
		return msg
	}
	if msg := h.InstallAPK(apk); msg != "" {
		os.Remove(apk)
		return msg
	}
	return ""
}
