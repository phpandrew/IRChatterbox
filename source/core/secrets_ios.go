//go:build ios

package core

// iOS: the app's sandbox is only readable by the app, and its files are encrypted while the
// phone is locked; passwords stay in config.json there, as on Android.
func osSecretBackend() secretBackend { return nil }
