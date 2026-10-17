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

The version comes from `source\VERSION` (embedded at build time), so a plain `wails build` of `0.3.0` shows as `0.3.0-dev` in Settings → About. To build an exact version string instead:

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
| Push to `main` | Build, then the security scans. If both pass, a new release is published automatically |
| Push a tag `vX.Y.Z` | Release `vX.Y.Z` is published (the tag must match `VERSION`) |

### Versions

`source/VERSION` holds `MAJOR.MINOR.PATCH`, e.g. `0.3.0`. You only edit it to start a new minor or major version.

| Build | Version |
|---|---|
| First release from `main` | exactly `VERSION`, e.g. `0.3.0` |
| Every later push to `main` | the next patch, counted from the release tags: `0.3.1`, `0.3.2`, … |
| Branches and pull requests | a pre-release of the next version, e.g. `0.3.3-dev.4+abc1234` |
| Local `wails build` | `0.3.0-dev` |

**Releasing is automatic.** Push to `main` (or merge a pull request), and the Build workflow builds the exe and runs govulncheck, npm audit and CodeQL. If everything passes it tags the next version and publishes the release with generated notes. The in-app updater offers it to everyone within six hours. If a check fails, nothing is released; fix it and push again.

To start a new minor or major version, change `VERSION` to `0.4.0` and push. That push becomes `v0.4.0`, and patches count up from there. A `VERSION` with a suffix such as `0.4.0-beta.1` is released once as a pre-release, which the updater skips.

Pushes that only change documentation (`*.md`, `docs/`, screenshots) don't build or release.

The updater checks the latest non-pre-release release, downloads `IRChatterbox.exe`, checks it against `SHA256SUMS.txt` and swaps it in. Keep those two asset names unchanged.

## Security checks

The full flow, with a diagram, is in **[SECURITY-FLOW.md](SECURITY-FLOW.md)**.

The **Security** workflow (`.github/workflows/security.yml`) runs daily, on every push to `main` and on pull requests:

| Scanner | Looks for |
|---|---|
| `govulncheck` | Known vulnerabilities in Go modules and the Go standard library that the code actually calls |
| `npm audit` | Known vulnerabilities in the frontend's npm packages |
| CodeQL | Bugs in our own code: XSS, injection, path traversal and so on |

Each scanner keeps one issue up to date, labelled `security`. It opens when something is found, gets a comment when the findings change, and closes itself once a scan comes back clean. On pull requests no issues are opened; the check fails instead. Dependabot (`.github/dependabot.yml`) opens weekly pull requests for dependency updates.

Run the same scans locally:

```powershell
cd source
$env:GOOS="windows"; go run golang.org/x/vuln/cmd/govulncheck@latest ./...
cd frontend; npm audit
```

## Project layout

All paths are under `source/`.

| Path | What |
|---|---|
| `VERSION` | The version number (see Versions above) |
| `main.go` | Desktop entry point: embeds the UI and `VERSION`, starts `core.RunDesktop` |
| `core/` | Everything shared by Windows and Android (package `core`) |
| `core/desktop.go` | Window options, lifecycle, dialogs, startup flags (`--minimized`, `--wait-pid`) — not built for Android |
| `core/host_android.go` | The Android host: events, notifications, background/keep-alive — Android only |
| `core/app.go` | Methods the UI calls, notifications, attention (tray/taskbar), auto-away |
| `core/irc.go`, `core/ircmsg.go` | IRC connection: TLS, CAP/SASL, parsing, channel and user state, events |
| `core/bouncer.go` | soju/ZNC: history replay, read markers |
| `core/commands.go` | `/command` handling |
| `core/encoding.go` | Per-server text encodings (UTF-8 with fallback, Cyrillic, Japanese, …) |
| `core/plugins.go` | Built-in bots: temperature converter, URL commands, `!music` |
| `core/scripts.go` | JavaScript plugin engine (see `docs/PLUGINS.md`) |
| `core/nowplaying*.go` | Reading the current song from Windows media controls, MusicBee or a text file |
| `core/logging.go` | History (`.jsonl`) and text logs (`.txt`), trimming |
| `core/updater.go`, `core/updater_desktop.go` | Checking GitHub Releases; downloading and swapping the exe (desktop) |
| `core/platform_windows.go` | Tray icon, taskbar flash, idle time, toasts |
| `core/window_windows.go`, `core/autostart_windows.go` | Restoring window position and size, starting with Windows |
| `core/config.go` | `config.json`, themes |
| `mobile/` | gomobile binding for the Android app (see `docs/ANDROID.md`) |
| `frontend/src/main.js` | UI: sidebar, chat, user list, input, drag to reorder |
| `frontend/src/modals.js` | Settings, server editor, dialogs |
| `frontend/src/format.js` | mIRC formatting and colours, links, inline media, nick colours |
| `frontend/src/themes.js` | Built-in themes and theme handling |
| `frontend/src/emoji.js`, `sounds.js` | Emoji picker, notification sounds |
| `frontend/src/mobile.js` | Phone layout: drawers, back button, swipe gestures (Android and narrow windows) |

The Android shell lives in `android/` at the repo root; see [ANDROID.md](ANDROID.md).

## Where user data lives

`%APPDATA%\IRChatterbox\` holds `config.json`, `themes\`, `plugins\` (with `plugins\data\` for plugin storage) and `logs\`.
