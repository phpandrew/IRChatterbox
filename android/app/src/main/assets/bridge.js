// Injected at document start by MainActivity, for the app's own origin only. Gives the
// shared frontend the same two objects Wails provides on Windows, backed by the Go core:
//   window.go.core.App.<Method>(...args) → Promise
//   window.runtime.EventsOn(event, fn)
// IcbNative is a WebMessageListener object: Android only accepts its messages from the
// app's main frame, so embedded players (YouTube, Vimeo, …) can't reach the core.
(() => {
  const N = window.IcbNative;
  if (!N || window.go) return;
  const send = (msg) => N.postMessage(JSON.stringify(msg));

  let seq = 0;
  const pending = new Map();
  const listeners = Object.create(null);
  // phone state the settings page shows; MainActivity pushes updates
  const status = { battery: true, notifications: true };

  const App = new Proxy(Object.create(null), {
    get(_, name) {
      if (typeof name !== "string" || name === "then") return undefined;
      return (...args) =>
        new Promise((resolve, reject) => {
          const id = ++seq;
          pending.set(id, { resolve, reject });
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

  // Called by MainActivity.
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
    // Android back button: the UI closes whatever is open and returns true, or returns
    // false and the app goes to the background.
    back() {
      const handler = window.__icbBack;
      try { return !!(handler && handler()); } catch (e) { console.error(e); return false; }
    },
  };

  // The UI calls this once its event listeners are registered (end of boot in main.js),
  // so a notification tap or share delivered at launch isn't lost.
  window.__icbReady = () => send({ t: "ready" });

  // Platform helpers the mobile UI uses (settings rows, system bar colours).
  window.icbAndroid = {
    setSystemBars(color, light) { send({ t: "bars", color: String(color), light: !!light }); },
    batteryUnrestricted() { return status.battery; },
    requestUnrestricted() { send({ t: "battery" }); },
    notificationsAllowed() { return status.notifications; },
    requestNotifications() { send({ t: "notifications" }); },
    openNotificationSettings() { send({ t: "notificationSettings" }); },
    vibrate(ms) { send({ t: "vibrate", ms: ms | 0 }); },
  };
})();
