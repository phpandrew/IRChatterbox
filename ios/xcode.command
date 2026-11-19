#!/usr/bin/env bash
# One-step iOS dev build: frontend -> Go core (IrcCore.xcframework) -> Xcode project -> open in Xcode.
# Double-click in Finder, or run from a terminal:
#   ios/xcode.command              full rebuild, then open Xcode
#   ios/xcode.command --fast       reuse the existing frontend build and Go framework (just regen + open)
#   ios/xcode.command --sim        build the Go core for the simulator only (faster gomobile)
#   ios/xcode.command --build      build for the simulator with xcodebuild instead of opening Xcode
# Needs: Node/npm, Go, gomobile (see build-core.sh), Xcode, and XcodeGen (brew install xcodegen).
set -euo pipefail

# Finder-launched shells may miss these.
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/.local/xcodegen/bin:$HOME/go/bin:$PATH"

here="$(cd "$(dirname "$0")" && pwd)"
root="$here/.."
fast=0; build=0
for arg in "$@"; do
  case "$arg" in
    --fast)  fast=1 ;;
    --sim)   export TARGETS=iossimulator ;;
    --build) build=1 ;;
    *) echo "Unknown option: $arg" >&2; exit 2 ;;
  esac
done

for tool in npm go gomobile xcodegen xcodebuild; do
  command -v "$tool" >/dev/null || { echo "Missing: $tool" >&2; exit 1; }
done

if [ "$fast" -eq 0 ] || [ ! -f "$root/source/frontend/dist/index.html" ]; then
  echo "==> Building frontend"
  cd "$root/source/frontend"
  [ -d node_modules ] || npm ci
  npm run build
fi

if [ "$fast" -eq 0 ] || [ ! -d "$here/Frameworks/IrcCore.xcframework" ] || [ ! -d "$here/build/web" ]; then
  echo "==> Building Go core"
  bash "$here/build-core.sh"
fi

echo "==> Generating Xcode project"
cd "$here"
xcodegen generate

if [ "$build" -eq 1 ]; then
  echo "==> Building for simulator"
  xcodebuild -project IRChatterbox.xcodeproj -scheme IRChatterbox -configuration Debug \
    -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO build
else
  open IRChatterbox.xcodeproj
fi
