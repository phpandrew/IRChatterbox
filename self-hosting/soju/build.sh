#!/bin/sh
# Builds soju v0.11.1 with IRChatterbox's encrypted-history patch.
# Needs git and Go 1.22+. Output: ./out/soju and ./out/sojuctl (static, pure-Go SQLite).
set -eu
here=$(cd "$(dirname "$0")" && pwd)
version=v0.11.1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$version" https://codeberg.org/emersion/soju.git "$work/soju"
cd "$work/soju"
git apply "$here/irchatterbox-soju-encryption.patch"

mkdir -p "$here/out"
export CGO_ENABLED=0
go build -trimpath -tags=moderncsqlite -ldflags "-X codeberg.org/emersion/soju/config.DefaultPath=/etc/soju/config" -o "$here/out/soju" ./cmd/soju
go build -trimpath -tags=moderncsqlite -ldflags "-X codeberg.org/emersion/soju/config.DefaultPath=/etc/soju/config" -o "$here/out/sojuctl" ./cmd/sojuctl
echo "Built: $here/out/soju and $here/out/sojuctl (soju $version + IRChatterbox encryption)"
