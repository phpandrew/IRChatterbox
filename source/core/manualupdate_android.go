//go:build android

package core

// Android: the app downloads the APK itself and hands it to the system installer
// (InstallUpdate in host_android.go); "manual" here means the UI says so instead of
// "Update & restart".
const manualUpdate = true
const hasTray = false
