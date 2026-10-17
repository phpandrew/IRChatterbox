package main

import (
	"embed"
	_ "embed"

	"irchatterbox/core"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed VERSION
var versionFile string

// appVersion is normally empty; CI sets it with -ldflags "-X main.appVersion=0.4.2".
var appVersion string

func main() {
	core.SetVersion(appVersion, versionFile)
	core.RunDesktop(assets)
}
