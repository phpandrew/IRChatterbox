//go:build darwin

package core

// the release file for macOS: a zipped .app. It isn't swapped in place (an app bundle is a
// folder, and macOS checks it on first launch), so the updater opens the download instead.
const updateAsset = "IRChatterbox-macos.zip"

const manualUpdate = true
const hasTray = false
