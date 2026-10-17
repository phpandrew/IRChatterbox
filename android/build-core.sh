#!/usr/bin/env bash
# Builds the Go core (source/core via source/mobile) into app/libs/irchatterbox-core.aar.
# Phones only by default; add an emulator with TARGETS=android/arm64,android/arm,android/amd64
# Needs Go, the Android NDK (ANDROID_NDK_HOME) and gomobile:
#   go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
cd "$here/../source"
mkdir -p "$here/app/libs"
gomobile bind \
  -target="${TARGETS:-android/arm64,android/arm}" \
  -androidapi 26 \
  -javapkg=io.github.phpandrew.irchatterbox.core \
  -trimpath -ldflags "-s -w" \
  -o "$here/app/libs/irchatterbox-core.aar" \
  ./mobile
rm -f "$here/app/libs/irchatterbox-core-sources.jar"
echo "Built app/libs/irchatterbox-core.aar"
