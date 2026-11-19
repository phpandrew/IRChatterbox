// Injected at document start by WebViewController, into the app's own page only (main
// frame). Gives the shared frontend the same two objects Wails provides on Windows, backed
// by the Go core:
//   window.go.core.App.<Method>(...args) → Promise
//   window.runtime.EventsOn(event, fn)
// It's the iOS twin of android/app/src/main/assets/bridge.js. Messages go to Swift through
// window.webkit.messageHandlers.icb, which only accepts them from the app's main frame, so
// embedded players (YouTube, Vimeo, …) can't reach the core.
(() => {
  const H = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.icb;
  if (!H || window.go) return;
  const send = (msg) => H.postMessage(JSON.stringify(msg));

  // Page errors go to the Xcode console (the shell prints "log" messages), so a failure during
  // startup shows up without opening Safari's Web Inspector.
  const log = (text) => send({ t: "log", text: String(text) });
  addEventListener("error", (e) => log(`JS error: ${e.message} at ${e.filename}:${e.lineno}:${e.colno}`));
  addEventListener("unhandledrejection", (e) => log("unhandled promise: " + (e.reason?.stack || e.reason)));
  const consoleError = console.error.bind(console);
  console.error = (...a) => { log("console.error: " + a.map((x) => x?.stack || String(x)).join(" ")); consoleError(...a); };

  let seq = 0;
  const pending = new Map();
  const listeners = Object.create(null);
  // phone state the settings page shows; Swift pushes updates
  const status = { battery: true, notifications: true };

  const App = new Proxy(Object.create(null), {
    get(_, name) {
      if (typeof name !== "string" || name === "then") return undefined;
      return (...args) =>
        new Promise((resolve, reject) => {
          const id = ++seq;
          // a call the core never answers stalls whatever awaits it; say so in the Xcode console
          const slow = setTimeout(() => log(`core call ${name} still unanswered after 5s`), 5000);
          pending.set(id, {
            resolve: (v) => { clearTimeout(slow); resolve(v); },
            reject: (e) => { clearTimeout(slow); log(`core call ${name} failed: ${e.message}`); reject(e); },
          });
          send({ t: "call", id, m: name, a: JSON.stringify(args) });
        });
    },
  });

  window.go = { core: { App } };

  window.runtime = {
    EventsOn(event, fn) {
      (listeners[event] ||= []).push(fn);
      return () => window.runtime.EventsOff(event, fn);
    },
    EventsOff(event, fn) {
      if (!fn) delete listeners[event];
      else listeners[event] = (listeners[event] || []).filter((f) => f !== fn);
    },
    EventsEmit(event, ...data) {
      (listeners[event] || []).slice().forEach((f) => f(...data));
    },
    WindowSetTitle() {},
    BrowserOpenURL(url) { send({ t: "openUrl", url: String(url) }); },
  };

  // Called by Swift.
  window.__icb = {
    resolve(id, reply) {
      const p = pending.get(id);
      if (!p) return;
      pending.delete(id);
      if (reply && "error" in reply) p.reject(new Error(reply.error));
      else p.resolve(reply ? reply.ok : null);
    },
    emit(batch) {
      for (const [event, data] of batch) {
        for (const fn of (listeners[event] || []).slice()) {
          try { fn(data); } catch (e) { console.error(e); }
        }
      }
    },
    status(s) {
      Object.assign(status, s);
      (listeners["androidStatus"] || []).slice().forEach((f) => f(status));
    },
  };

  // The UI calls this once its event listeners are registered (end of boot in main.js),
  // so a notification tap delivered at launch isn't lost.
  window.__icbReady = () => send({ t: "ready" });

  // The phone UI is written against the Android shell's helper object; iOS provides the same
  // one (so the phone layout, backups and log sharing work unchanged) and flags itself with
  // icbIOS so the few Android-only settings can hide.
  window.icbIOS = true;
  window.icbAndroid = {
    setSystemBars(color, light) { send({ t: "bars", color: String(color), light: !!light }); },
    batteryUnrestricted() { return true; }, // iOS has no battery optimisation to turn off
    requestUnrestricted() {},
    notificationsAllowed() { return status.notifications; },
    requestNotifications() { send({ t: "notifications" }); },
    openNotificationSettings() { send({ t: "notificationSettings" }); },
    vibrate(ms) { send({ t: "vibrate", ms: ms | 0 }); },
    // settings backup: the answer arrives as an "androidBackup" event
    saveBackup(name, data) { send({ t: "saveBackup", name: String(name), data: String(data) }); },
    openBackup() { send({ t: "openBackup" }); },
    shareLogs() { send({ t: "shareLogs" }); },
  };
})();
