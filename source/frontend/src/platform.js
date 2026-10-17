// Which desktop OS the app runs on ("windows", "darwin", "linux"), from the Wails runtime.
// Adds os-<name> to <body> so Windows-only settings (tray, start with Windows) can hide.
export let os = "windows";
export async function detectOS() {
  try {
    const env = await window.runtime?.Environment?.();
    if (env?.platform) os = env.platform;
  } catch {}
  document.body.classList.add("os-" + os);
  return os;
}
