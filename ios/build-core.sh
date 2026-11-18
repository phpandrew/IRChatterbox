#!/usr/bin/env bash
# Builds the Go core (source/core via source/mobile) into ios/Frameworks/IrcCore.xcframework
# and copies the built frontend (source/frontend/dist) to ios/build/web, which the app bundles.
# Runs on a Mac (CI: the ios job in .github/workflows/release.yml). Needs Go, Xcode and gomobile:
#   go install golang.org/x/mobile/cmd/gomobile@latest golang.org/x/mobile/cmd/gobind@latest
#   gomobile init
# Build the frontend first: cd source/frontend && npm ci && npm run build
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
dist="$here/../source/frontend/dist"
if [ ! -f "$dist/index.html" ]; then
  echo "Build the frontend first: cd source/frontend && npm ci && npm run build" >&2
  exit 1
fi

cd "$here/../source"
mkdir -p "$here/Frameworks"
rm -rf "$here/Frameworks/IrcCore.xcframework"
gomobile bind \
  -target="${TARGETS:-ios,iossimulator}" \
  -iosversion 16.0 \
  -trimpath -ldflags "-s -w" \
  -o "$here/Frameworks/IrcCore.xcframework" \
  ./mobile

rm -rf "$here/build/web"
mkdir -p "$here/build/web"
cp -R "$dist/." "$here/build/web/"
rm -f "$here/build/web/gitkeep" "$here/build/web/.gitkeep"
echo "Built Frameworks/IrcCore.xcframework and build/web"
