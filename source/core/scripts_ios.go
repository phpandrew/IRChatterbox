//go:build ios

package core

// Apple doesn't allow apps to run downloaded code that adds features (App Review
// Guideline 2.5.2), so JavaScript and mIRC script plugins are off on iOS. The built-in
// plugins (weather, URL commands) still work.
func init() { scriptsUnavailable = "Script plugins aren't available on iOS" }
