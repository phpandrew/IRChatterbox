//go:build ios

// iOS-only part of the phone host (the shared part is host_mobile.go). The iOS shell is in
// ios/. TestFlight and the App Store deliver updates, so the GitHub update check is off
// (the shell calls SetStore("appstore")) and nothing is downloaded or installed in the app.

package core

// iOS has no release file on GitHub; verifyDownload never runs here.
const updateAsset = ""

// iOS suspends apps in the background, so the connection can't be kept live there.
const keepAliveSupported = false

// InstallUpdate isn't reachable on iOS (CheckForUpdate never reports one); if it's called,
// it says where updates come from.
func (a *App) InstallUpdate() string {
	return "Updates are installed by TestFlight or the App Store."
}
