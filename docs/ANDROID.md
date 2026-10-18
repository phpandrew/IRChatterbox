# IRChatterbox for Android

The Android app is the same IRChatterbox: the same Go core (IRC, bouncer support, SASL,
encodings, logging, plugins) and the same UI, laid out for a phone.

## Install

Download `IRChatterbox.apk` from the [Releases](../../releases) page on your phone and open it.
Android asks once to allow installs from your browser. After that, the app tells you
when there's a new version. **Download update** opens the new APK, and installing it keeps your
servers and settings.

[Obtainium](https://github.com/ImranR98/Obtainium) can also watch the repo and update the app.

## Using it

- **Channels**: tap ☰ or swipe right. **Users**: tap the people icon or swipe left. The back
  button closes whatever is open.
- **Long-press** a nick, channel, server or link for the same menus you get from right-clicking on Windows.
- **⇥** beside the message box completes nicks, like Tab on Windows.
- **Notifications** stack per channel or private chat. Reply straight from the notification.
- **Share** text or a link from another app to IRChatterbox to drop it into the message box.

### Background (Settings → General → Background)

- **Keep connection live in background** (on by default) keeps you connected after you
  leave the app, and shows a small "Connected" notification while it does. The notification's
  **Disconnect** button drops every connection.
- With it off, the app disconnects when you leave it and reconnects when you come back.
  Through soju you still get what you missed, and read markers stay in sync with Windows.
- **Battery: Allow** asks Android not to optimise IRChatterbox. Without it, some phones cut
  the connection after a while with the screen off. Through a bouncer that only means a
  reconnect, but direct connections drop.

### Not on Android

The tray, start with Windows, window state, the MusicBee `!music` / `/np` plugin, custom
notification sound files, text-log folders and JavaScript plugins (their folder is private to
the app) are Windows-only. Weather and URL commands work the same.

## How it's built

| Piece | Where |
|---|---|
| Go core shared with Windows | `source/core/` |
| Android binding (gomobile) | `source/mobile/` → `android/app/libs/irchatterbox-core.aar` |
| Kotlin shell: WebView, notifications, keep-alive service | `android/app/src/main/java/…` |
| Bridge that gives the UI `window.go.core.App` and `window.runtime` | `android/app/src/main/assets/bridge.js` |
| Shared UI, with the phone layout in `mobile.js` and the end of `style.css` | `source/frontend/` |

Every push builds the APK in GitHub Actions next to the exe, and releases include both.

### Building it yourself

Android Studio isn't needed. You need Go, Node, a JDK (17+), the Android SDK and NDK
(`sdkmanager "platforms;android-36" "build-tools;35.0.0" "ndk;27.2.12479018"`) and gomobile:

    go install golang.org/x/mobile/cmd/gomobile@latest
    gomobile init

Then:

    cd source/frontend && npm ci && npm run build && cd ../..
    android/build-core.sh                 # Go core → android/app/libs/irchatterbox-core.aar
    cd android && ./gradlew assembleGithubDebug # → app/build/outputs/apk/github/debug/app-github-debug.apk

There are two flavors:

- `github`: the APK on GitHub Releases, with the built-in update check
  (`./gradlew assembleGithubRelease`).
- `play`: the Google Play bundle, built with `./gradlew bundlePlayRelease`, which writes
  `app/build/outputs/bundle/playRelease/app-play-release.aab`. Play delivers its updates, so
  the update check is off, and it leaves out the restricted `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`
  permission: "Allow" opens the app's settings page instead. CI builds both, and the `.aab` is the
  `play-bundle-<version>` artifact on the workflow run.

Set `ANDROID_HOME` and `ANDROID_NDK_HOME` (or put `sdk.dir=…` in `android/local.properties`).
`adb install -r app/build/outputs/apk/github/debug/app-github-debug.apk` puts it on a phone with USB debugging.
The debug build installs alongside the release one (`….irchatterbox.debug`).

### The UI in a browser

`cd source/frontend && npm run dev` runs the UI with a mock backend. Narrow the window below
760px, or use the browser's device mode, to work on the phone layout.

## Release signing (one-time setup)

Android only installs an update over an existing app if both are signed with the same key.
Make the key once and keep a backup somewhere safe, because losing it means users must uninstall to update.

1. Create the key (`keytool` comes with any JDK):

       keytool -genkeypair -v -keystore irchatterbox.jks -alias irchatterbox -keyalg RSA -keysize 4096 -validity 10000

2. Add four repository secrets (GitHub → Settings → Secrets and variables → Actions):

   | Secret | Value |
   |---|---|
   | `ANDROID_KEYSTORE_BASE64` | the .jks file as base64. In PowerShell: `[Convert]::ToBase64String([IO.File]::ReadAllBytes("irchatterbox.jks")) \| Set-Clipboard` |
   | `ANDROID_KEYSTORE_PASSWORD` | the keystore password |
   | `ANDROID_KEY_ALIAS` | `irchatterbox` |
   | `ANDROID_KEY_PASSWORD` | the key password (the same as the keystore's unless you chose another) |

Without these secrets, a release build fails. Test builds (not releases) are signed with a
throwaway debug key, which is different on every run, so they won't update an installed release.

For local release builds, create `android/keystore.properties` (it's git-ignored):

    storeFile=irchatterbox.jks
    storePassword=…
    keyAlias=irchatterbox
    keyPassword=…
