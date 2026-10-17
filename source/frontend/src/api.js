// Thin wrapper over the backend bindings. The same UI runs in three hosts:
//  - Windows (Wails): window.go.core.App + window.runtime
//  - Android: the shell injects the same two objects (android/…/bridge.js)
//  - a plain browser, with the mock backend, for UI development
let mock = null;

const backend = () => window.go?.core?.App;

export async function initApi() {
  if (!backend()) {
    mock = await import("./mock.js");
    mock.install();
  }
}

export const Go = new Proxy({}, {
  get: (_, name) => (...args) => {
    const real = backend();
    if (real && real[name]) return real[name](...args);
    if (mock?.api[name]) return Promise.resolve(mock.api[name](...args));
    return Promise.resolve(null);
  },
});

export function on(event, fn) {
  if (window.runtime?.EventsOn) window.runtime.EventsOn(event, fn);
  else mock?.on(event, fn);
}

export function setTitle(t) {
  if (window.runtime?.WindowSetTitle) window.runtime.WindowSetTitle(t);
  else document.title = t;
}
