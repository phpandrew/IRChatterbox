// Thin wrapper over the Wails bindings so the UI can also run in a plain browser with a mock backend.
let mock = null;

export async function initApi() {
  if (!window.go?.main?.App) {
    mock = await import("./mock.js");
    mock.install();
  }
}

export const Go = new Proxy({}, {
  get: (_, name) => (...args) => {
    const real = window.go?.main?.App;
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
