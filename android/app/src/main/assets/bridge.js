// Injected at document start by MainActivity. Gives the shared frontend the same two
// objects Wails provides on Windows, backed by the Go core through IcbNative:
//   window.go.core.App.<Method>(...args) → Promise
//   window.runtime.EventsOn(event, fn)
(() => {
  const N = window.IcbNative;
  if (!N || window.go) return;

  let seq = 0;
  const pending = new Map();
  const listeners = Object.create(null);

  const App = new Proxy(Object.create(null), {
    get(_, name) {
      if (typeof name !== "string" || name === "then") return undefined;
      return (...args) =>
        new Promise((resolve, reject) => {
          const id = ++seq;
          pending.set(id, { resolve, reject });
          N.call(id, name, JSON.stringify(args));
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
    BrowserOpenURL(url) { N.openUrl(String(url)); },
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
    // Android back button: the UI closes whatever is open and returns true, or returns
    // false and the app goes to the background.
    back() {
      const handler = window.__icbBack;
      try { return !!(handler && handler()); } catch (e) { console.error(e); return false; }
    },
  };

  // Platform helpers the mobile UI uses (settings rows, system bar colours).
  window.icbAndroid = {
    setSystemBars(color, light) { N.setSystemBars(String(color), !!light); },
    batteryUnrestricted() { return N.batteryUnrestricted(); },
    requestUnrestricted() { N.requestUnrestricted(); },
    notificationsAllowed() { return N.notificationsAllowed(); },
    requestNotifications() { N.requestNotifications(); },
    openNotificationSettings() { N.openNotificationSettings(); },
    vibrate(ms) { N.vibrate(ms | 0); },
  };
})();
