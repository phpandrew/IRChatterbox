package main

import (
	_ "embed"
	"strings"
)

// VERSION (next to go.mod) is the single source of truth for the version number.
// Release builds get it exactly ("0.3.0"); every other build is a pre-release of it
// ("0.3.0-dev" locally, "0.3.0-dev.7+abc1234" from CI) so it ranks *below* the
// release and the updater offers the real thing once it's published.
//
//go:embed VERSION
var versionFile string

// appVersion is normally left empty and worked out from VERSION. CI sets the full
// string with -ldflags "-X main.appVersion=0.3.0-dev.7+abc1234".
var appVersion string

func init() {
	if appVersion == "" {
		appVersion = strings.TrimSpace(versionFile) + "-dev"
	}
}
