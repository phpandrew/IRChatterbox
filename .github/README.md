<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.png">
    <img alt="IRChatterbox" src="assets/logo-light.png" width="520">
  </picture>
</p>

A modern IRC client for Windows. The backend is Go and the UI is built with Wails (WebView2).

## Screenshots
<p align="center">
  <img src="assets/screenshot.png" alt="IRChatterbox connected to Libera.Chat, showing the server MOTD and a highlighted NickServ notice" width="900">
</p>
<p align="center">
  <img src="assets/screenshot-settings.png" alt="The Appearance settings with automatic light/dark mode and the theme editor" width="900">
</p>

## Run it
Double-click `IRChatterbox.exe`. No install is needed. It uses WebView2, which comes with Windows 10 and 11.
Settings, logs, themes and plugins live in `%APPDATA%\IRChatterbox` (Settings → Open data folder).

## Features
- **Saved servers and auto-login.** Each server can use SASL PLAIN (recommended) or NickServ IDENTIFY, connect on startup, auto-join channels (with keys) and run commands on connect. It reconnects automatically with backoff.
- **Sidebar.** Servers collapse to show the channels and queries under them. Unread counts appear in grey. Pings and PMs appear in red, and a collapsed server shows a combined badge.
- **Highlights.** Your nick and any highlight words trigger three things: a coloured line, a red badge and a Windows toast. Clicking the toast opens that channel.
- **Inline media.** PNG, JPG, GIF, WEBP and AVIF links, plus imgur. YouTube links show a thumbnail and play in the chat when clicked. You can turn either off in Settings → Media.
- **Nick colours.** Colours are automatic per nick. Override one with `/color nick #ff8800`, `/color nick orange` or `/color nick reset`, or right-click a nick.
- **Commands.** Every IRC command works, and `/help` lists the built-in ones. Anything the client doesn't recognise is sent to the server as-is (`/knock #chan` → `KNOCK #chan`).
- **Themes.** Automatic mode follows Windows light/dark (Settings → Personalisation → Colours → app mode) and switches live. Built-in themes are Dark, Light, Midnight and Classic. Settings → Appearance has a theme editor, and you pick which light and dark theme auto mode uses.
- **Logs and history.** Recent history reloads when you open a channel. You can also save plain-text `.txt` logs per channel (`[2026-10-16 09:41:22] <@Andrew> hello`) to a folder you choose, with a per-channel line limit (Settings → General).
- **Remembers the window.** Size, position, monitor, maximised and full-screen state (F11) are restored on launch.
- **Channel right-click.** "Join on connect" adds a channel to the auto-join list. "Show at startup" opens IRChatterbox on that channel instead of where you left off.
- **Start with Windows.** Optionally start minimised (Settings → General).
- **Auto-update.** New releases are found on GitHub and installed with one click. The download is checked against its SHA-256 sum, and the app restarts itself.
- **System tray.** The tray icon and taskbar button flash when you're mentioned or messaged while in another app. Optionally, closing the window hides it to the tray.
- **Notification sounds.** Five built-in sounds or your own .wav/.mp3/.ogg, set separately for highlights and private messages, with a volume control.
- **Auto-away.** You're marked away on every network after a set number of idle minutes, and back when you return.
- **Paste protection.** Before you send a big paste you can send it, join it into one line, or cancel.
- **Older networks.** Text encoding can be set per server: automatic UTF-8 with a Western fallback, or Cyrillic, Central European, Greek, Turkish, Japanese, Chinese, Korean and more.
- **Drag to reorder.** Drag servers and channels in the sidebar into any order. Channels are also joined in that order.
- **Quit confirmation.** Closing the window asks before disconnecting you. Tick "Don't ask me again" or turn it off in Settings → General.
- **Topics.** Double-click a channel topic to change it.
- **Marker line.** A red line divides messages you've read from ones that arrived while you were in another channel or another app. You can turn it off in Settings → General.
- **Jump to latest.** When you scroll up, a round arrow takes you back to the newest messages and counts new lines below. Ctrl+End does the same.
- **Emoji picker.** The smiley button beside Send (or Ctrl+E) opens a searchable picker with categories and a recently-used row. Shift+click adds several. It works offline.
- **Text shortcuts.** Type `$whatever` and it becomes `¯\_(ツ)_/¯`. Shortcuts expand when you press space or send, and typing `$` lists them. Edit them in Settings → Shortcuts or with `/shortcut add $name text`. The replacement can also be a command, such as `/me waves`.
- **Resizable nick column.** Drag the line between nicks and messages, or double-click it to fit the longest nick.
- **Keyboard.** Ctrl+K jumps to a channel. Ctrl+F filters the buffer. Alt+↑/↓ and Ctrl+Tab switch buffers, and Alt+A goes to the next unread. Tab completes nicks, ↑/↓ recall input history, and Ctrl+B/I/U add bold, italic and underline.

## Plugins
**Temperature converter (built in).** "Today it feels like 18c" → `[Bot] 18 C = 64.4 F`. It also handles °C, F, "fahrenheit", decimals and negatives. It reacts to your messages and other people's (each can be switched off), can be limited to certain channels, and never replies to lines that start with the bot prefix, so two bots won't loop.

**Now playing (`!music`, `/np`).** When anyone types `!music`, the bot replies with your current song: `[Bot] Now playing: Artist - Title`. `/np` sends `* you is listening to Artist - Title`. The song is read from Windows media controls, from MusicBee's window title, or from the text file written by MusicBee's [Now Playing to External Files](https://getmusicbee.com/addons/plugins/47/now-playing-to-external-files/) plugin. Settings → Plugins → **Test now** shows which of these works on your PC.

**URL commands (`!solar`).** Settings → Plugins. Set a trigger and a URL. By default only you can trigger them; untick "Only I can trigger it" to let others use one. Plain text or HTML replies are posted as up to N lines. JSON can be shaped with a Go template:

    Solar: {{.power}} W · today {{round .today_kwh 1}} kWh · {{kw .power}} kW

`{args}` in the URL is replaced by whatever follows the trigger (`!weather london`). Use the **Test** button to preview the output.

**JavaScript plugins.** Put `.js` files in `%APPDATA%\IRChatterbox\plugins`, then type `/reload`. Plugins can react to messages, joins, parts, kicks, nick changes, topics and more, add their own `/commands`, use timers, store data and fetch web pages. **[Plugin guide and API reference →](../docs/PLUGINS.md)**

## Download
Get `IRChatterbox.exe` from the [Releases](../../releases) page:
- **Versioned releases** (v0.3.0 …) are the stable builds.
- **Nightly build** is a pre-release that is rebuilt from every commit to `main`.

### Making a release
Push a version tag and GitHub builds the exe and publishes the release with notes:

    git tag v0.3.0
    git push origin v0.3.0

A tag with a suffix, such as `v0.3.0-beta1`, is published as a pre-release. Every other push or pull request is also built; the exe is on the Actions run page for 30 days.

## Build from source
See **[docs/BUILDING.md](../docs/BUILDING.md)** for requirements, building, live-reload development, running the tests and how releases work.

    cd source
    wails build
