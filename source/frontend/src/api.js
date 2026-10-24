// Thin wrapper over the backend bindings. The same UI runs in four hosts:
//  - Windows (Wails): window.go.core.App + window.runtime
//  - Android: the shell injects the same two objects (android/…/bridge.js)
//  - the web client (npm run build:web, i.e. vite --mode web), when its private source is present
//  - a plain browser, with the mock backend, for UI development
let mock = null;

// The web client (irchatterbox.com/chat) isn't part of this repository: its source sits in a
// private web-client/ folder beside it. Builds without that folder use the mock backend.
const webClient = import.meta.env.MODE === "web" ? import.meta.glob("../../../web-client/backend.js") : {};

const backend = () => window.go?.core?.App;

export async function initApi() {
  if (backend()) return;
  if (import.meta.env.MODE === "web") {
    const load = Object.values(webClient)[0];
    if (load) {
      mock = await load();
      await mock.install();
      return;
    }
  }
  mock = await import("./mock.js");
  mock.install();
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
