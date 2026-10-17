//go:build linux && !android

package core

// the release file the updater downloads and swaps in on this platform (a plain binary)
const updateAsset = "IRChatterbox-linux-amd64"

const manualUpdate = false
const hasTray = false
