# Building IRChatterbox

IRChatterbox is a [Wails v2](https://wails.io) app. The backend is Go (the IRC protocol, plugins, logging, updates and Windows integration) and the UI is plain JavaScript, HTML and CSS rendered by WebView2. There's no front-end framework.

## Requirements

| Tool | Version | Get it |
|---|---|---|
| Go | 1.26+ | https://go.dev/dl/ |
| Node.js | 18+ (22 recommended) | https://nodejs.org |
| Wails CLI | v2.16 | `go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0` |
| WebView2 runtime | any | Already part of Windows 10 and 11 |

Run `wails doctor` to check that everything is set up.

## Build

```powershell
git clone https://github.com/phpandrew/IRChatterbox
cd IRChatterbox\source
wails build
```

The exe is written to `source\build\bin\IRChatterbox.exe`. It's a single file with no installer.

To stamp a version into the build (shown in Settings → About, and used by the updater):

```powershell
wails build -ldflags "-X main.appVersion=0.3.0"
```

You can cross-compile from Linux or macOS with `wails build -platform windows/amd64`. The Windows parts (tray, notifications, registry) are pure Go, so no C compiler is needed.

## Develop with live reload

```powershell
cd source
wails dev
```

Front-end changes reload instantly, and Go changes trigger a rebuild. To work on just the UI in a normal browser with fake data:

```powershell
cd source\frontend
npm install
npx vite
```

`src/mock.js` stands in for the Go backend, with sample servers, channels, messages, images and a YouTube link.

## Tests

The integration tests connect to a real IRC server on `127.0.0.1` (plain 6667 and TLS 6697). [Ergo](https://ergo.chat) is the easiest to run locally:

```powershell
# one-time: get ergo, generate its config and a self-signed certificate
ergo defaultconfig > ircd.yaml
ergo mkcerts
# in ircd.yaml, set  enforce-utf8: false  (the encoding test sends Latin-1 bytes)
ergo run
```

Then, from `source`:

```powershell
go test -tags integration ./...
```

The tests cover connecting over TLS, SASL and NickServ logins, highlights, the temperature bot, URL commands, `!music`, op/kick/topic commands, plugin events, commands, timers and storage, and text encodings.

## Releases and CI

`.github/workflows/release.yml` builds on GitHub's Windows runners:

| Trigger | Result |
|---|---|
| Any push or pull request | The exe is saved as an artifact on the run (30 days) |
| Push to `main` | The **Nightly build** pre-release is replaced with this build |
| Push a tag `vX.Y.Z` | Release `vX.Y.Z` is published with notes, the exe, a zip and SHA256SUMS |
| Publishing a release on GitHub | The exe is attached to it |

To cut a release:

```powershell
git tag v0.3.0
git push origin v0.3.0
```

The app's auto-updater checks the latest non-pre-release release, downloads `IRChatterbox.exe`, checks it against `SHA256SUMS.txt` and swaps it in. Keep those two asset names unchanged.

Version numbers come from git: a tagged build is `0.3.0`, a later commit is `0.3.0-dev.4+abc1234`, and with no tags at all the version falls back to the `appVersion` in `app.go`.

## Project layout

All paths are under `source/`.

| Path | What |
|---|---|
| `main.go` | Window options, startup flags (`--minimized`, `--wait-pid`) |
| `app.go` | Methods the UI calls, notifications, attention (tray/taskbar), auto-away |
| `irc.go`, `ircmsg.go` | IRC connection: TLS, CAP/SASL, parsing, channel and user state, events |
| `commands.go` | `/command` handling |
| `encoding.go` | Per-server text encodings (UTF-8 with fallback, Cyrillic, Japanese, …) |
| `plugins.go` | Built-in bots: temperature converter, URL commands, `!music` |
| `scripts.go` | JavaScript plugin engine (see `docs/PLUGINS.md`) |
| `nowplaying*.go` | Reading the current song from Windows media controls, MusicBee or a text file |
| `logging.go` | History (`.jsonl`) and text logs (`.txt`), trimming |
| `updater.go` | Auto-update from GitHub Releases |
| `platform_windows.go` | Tray icon, taskbar flash, idle time, toasts |
| `window_windows.go`, `autostart_windows.go` | Restoring window position and size, starting with Windows |
| `config.go` | `config.json`, themes |
| `frontend/src/main.js` | UI: sidebar, chat, user list, input, drag to reorder |
| `frontend/src/modals.js` | Settings, server editor, dialogs |
| `frontend/src/format.js` | mIRC formatting and colours, links, inline media, nick colours |
| `frontend/src/themes.js` | Built-in themes and theme handling |
| `frontend/src/emoji.js`, `sounds.js` | Emoji picker, notification sounds |

## Where user data lives

`%APPDATA%\IRChatterbox\` holds `config.json`, `themes\`, `plugins\` (with `plugins\data\` for plugin storage) and `logs\`.
