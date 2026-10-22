//go:build android

package core

// Android: the app's private storage is already only readable by the app; passwords stay
// in config.json there.
func osSecretBackend() secretBackend { return nil }
