package core

import "strings"

// source/VERSION is the single source of truth for the version number. Release builds get
// it exactly ("0.3.0"); every other build is a pre-release of it ("0.3.0-dev" locally,
// "0.3.0-dev.7+abc1234" from CI) so it ranks *below* the release and the updater offers
// the real thing once it's published.
//
// The desktop main package embeds VERSION and passes it to SetVersion (CI still sets the
// full string with -ldflags "-X main.appVersion=…"). Android passes the version from Gradle,
// which reads the same file.
var appVersion = "0.0.0-dev"

// SetVersion sets the app version: full when given, otherwise VERSION's contents + "-dev".
func SetVersion(full, versionFile string) {
	if full = strings.TrimSpace(full); full != "" {
		appVersion = full
	} else if v := strings.TrimSpace(versionFile); v != "" {
		appVersion = v + "-dev"
	}
}
