//go:build ios

package core

// iOS: TestFlight and the App Store install updates; no tray.
const manualUpdate = true
const hasTray = false
