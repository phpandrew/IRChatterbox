# IRChatterbox for iOS

The iOS app is the same IRChatterbox as on Windows and Android: the same Go core and the same
UI, with the phone layout on iPhone and iPad (iOS 16 or later).

## Install (beta)

The beta is on TestFlight. Install the free **TestFlight** app from the App Store, then open
the invite link or email you were sent. TestFlight installs the new versions as they come out.

## Using it

The app works like the Android app: tap ☰ or swipe for channels, long-press for menus, and tap
a notification to open that chat. Settings backups and **Share app log** use the iOS save and
share sheets.

### When you leave the app

iOS pauses apps soon after they leave the screen, so IRChatterbox can't stay connected in the
background the way it can on Android. Instead it disconnects cleanly about 20 seconds after
you leave (a quick switch to another app doesn't drop anything), and reconnects when you come
back. **Connect through a bouncer** (your own, or the hosted IRC bouncer on irchatterbox.com) and
it replays what you missed and keeps read markers in sync. Without a bouncer you miss what's
said while the app is closed.

Notifications work while the app is open or in those first seconds after you leave it.

### Not on iOS

- **Keep connection live in background**, as above.
- **JavaScript and mIRC script plugins.** Apple doesn't allow apps to run downloaded code
  that adds features (App Review Guideline 2.5.2). The built-in weather and URL commands work.
- Everything that's also missing on Android: the tray, start with Windows, window state, the
  `!music` / `/np` plugin, custom sound files and text-log folders.

## How it's built

| Piece | Where |
|---|---|
| Go core shared with Windows and Android | `source/core/` (`host_mobile.go` for both phones, `host_ios.go` for iOS) |
| iOS binding (gomobile) | `source/mobile/` → `ios/Frameworks/IrcCore.xcframework` |
| Swift shell: WKWebView, notifications, backgrounding | `ios/IRChatterbox/` |
| Bridge that gives the UI `window.go.core.App` and `window.runtime` | `ios/IRChatterbox/bridge.js` |
| Xcode project (generated, not committed) | `ios/project.yml` (XcodeGen) |

The UI is served from inside the app at `icb://localhost/`. The bridge also provides the same
`window.icbAndroid` helpers as the Android shell, so the phone UI needs no iOS-only code apart
from hiding the Android-only settings (`body.ios`, `.not-ios` / `.ios-only` in `style.css`).

### CI and TestFlight

The `ios` job in `.github/workflows/release.yml` runs on a macOS runner, so no Mac is needed:

- **Releases from `main`** (and runs started by hand from the Actions tab) are signed and
  uploaded to TestFlight when these repository secrets are set
  (Settings → Secrets and variables → Actions):

  | Secret | What |
  |---|---|
  | `ASC_KEY_ID` | App Store Connect API key ID |
  | `ASC_ISSUER_ID` | Issuer ID shown above the list of keys |
  | `ASC_KEY_P8` | The whole text of the downloaded `AuthKey_….p8` file |
  | `APPLE_TEAM_ID` | Team ID (developer.apple.com → Membership details) |

- **Every other build** (branches, pull requests, or no secrets yet) is an unsigned build. It
  proves the app compiles, and `IRChatterbox-ios-unsigned.ipa` is on the run page for 30 days.
  Sideloadly or AltStore can install it with a free Apple ID (it stops working after 7 days).

The App Store version is the numeric part of the release (`0.8.3`). The build number is the
workflow run number, which goes up with every run. The iOS app isn't part of the GitHub
release, and a failed iOS upload never holds the release up.

Signing is automatic: with the API key, Xcode fetches or creates the distribution certificate
and App Store profile in Apple's cloud. No certificates or profiles are kept in the repo.

### One-time setup in Apple's portals

1. **Register the bundle ID** at developer.apple.com → Certificates, Identifiers & Profiles →
   Identifiers → **+** → App IDs → App: `io.github.phpandrew.irchatterbox`, named
   IRChatterbox. Leave the capabilities as they are.
2. **Create the app** in App Store Connect → Apps → **+** → New App: iOS, name IRChatterbox,
   the bundle ID from step 1, SKU `irchatterbox`, full access.
3. **Create an API key** in App Store Connect → Users and Access → Integrations → App Store
   Connect API → Team Keys → **+**, with the **Admin** role (needed for cloud signing).
   Download the `.p8` file; Apple only lets you download it once.
4. **Add the four secrets** above to the GitHub repository.
5. **Run it:** push to `main`, or Actions → Build → Run workflow. After the upload, the build
   takes 5–30 minutes to process, then appears under **TestFlight** in App Store Connect.
6. **Testers:**
   - **Internal** (up to 100 people on your App Store Connect team): TestFlight → Internal
     Testing → **+**. They get each build right away, with no review.
   - **External** (up to 10,000 people): TestFlight → External Testing → **+** to make a group,
     fill in the test information (what to test, a feedback email), and add the build. The first
     build of each version goes through Beta App Review, which usually takes about a day.
     Invite people by email, or turn on the **public link** and share it.

The first upload asks about export compliance only if the build doesn't answer it: the app
declares `ITSAppUsesNonExemptEncryption = NO` (it only uses standard TLS), so it doesn't.

If the upload step says it couldn't find a profile or certificate, check that the API key has
the Admin role and that the bundle ID and app record exist.

### Building on a Mac

You need Xcode 16+, Go, Node, gomobile and XcodeGen (`brew install xcodegen`):

    go install golang.org/x/mobile/cmd/gomobile@latest golang.org/x/mobile/cmd/gobind@latest
    gomobile init
    cd source/frontend && npm ci && npm run build && cd ../..
    ios/build-core.sh          # Go core → ios/Frameworks/IrcCore.xcframework, UI → ios/build/web
    cd ios && xcodegen generate && open IRChatterbox.xcodeproj

Choose your team under Signing & Capabilities and run it on a phone or the simulator. Debug
builds can be inspected from Safari's Develop menu.
