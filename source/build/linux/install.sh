#!/bin/sh
# Installs IRChatterbox for the current user: the app in ~/.local/bin, plus a menu entry and
# icon so it shows up in your desktop's app launcher. Run from the unpacked folder.
set -e
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor/256x256/apps"
install -m 755 "$here/IRChatterbox" "$HOME/.local/bin/IRChatterbox"
install -m 644 "$here/irchatterbox.png" "$HOME/.local/share/icons/hicolor/256x256/apps/irchatterbox.png"
sed "s|^Exec=IRChatterbox|Exec=$HOME/.local/bin/IRChatterbox|" "$here/irchatterbox.desktop" > "$HOME/.local/share/applications/irchatterbox.desktop"
command -v update-desktop-database >/dev/null && update-desktop-database "$HOME/.local/share/applications" || true
echo "Installed. Start IRChatterbox from your app menu, or run: ~/.local/bin/IRChatterbox"
