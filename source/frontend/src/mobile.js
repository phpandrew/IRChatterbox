// Phone layout: used by the Android app, and by any window narrower than PHONE_WIDTH.
// The sidebar and user list become slide-in drawers, the back button closes whatever is
// open, and the status/navigation bars follow the theme.
import { on } from "./api.js";

const $ = (id) => document.getElementById(id);
const PHONE_WIDTH = 760;
const narrow = matchMedia(`(max-width: ${PHONE_WIDTH}px)`);

export const android = !!window.icbAndroid;
export const isMobile = () => document.body.classList.contains("mobile");

let ctx = null; // functions from main.js

function setMode() {
  const m = android || narrow.matches;
  document.body.classList.toggle("mobile", m);
  document.body.classList.toggle("android", android);
  if (!m) closeDrawers();
}

export function openNav() { closeDrawers(); document.body.classList.add("nav-open"); }
export function openUsers() { closeDrawers(); document.body.classList.add("users-open"); }
export function closeDrawers() { document.body.classList.remove("nav-open", "users-open"); }
const drawerOpen = () => document.body.classList.contains("nav-open") || document.body.classList.contains("users-open");

// ---- hooks called from main.js ----

/** After a buffer is selected: close the channel drawer. */
export function afterSelect() {
  if (isMobile()) document.body.classList.remove("nav-open");
}

/** After the header renders: the users button only makes sense in channels. */
export function afterHeader(buffer) {
  const ch = buffer?.type === "channel";
  $("usersBtn").classList.toggle("m-hide", !ch);
  if (!ch) document.body.classList.remove("users-open");
}

/** After the tree renders: badge the menu button with what's waiting elsewhere. */
export function afterTree(servers, active) {
  let hl = 0, unread = 0;
  for (const srv of servers)
    for (const b of srv.buffers.values()) {
      if (active.server === srv.id && active.key === b.key) continue;
      hl += b.hl || 0;
      unread += b.unread || 0;
    }
  const el = $("navBadge");
  el.className = "nav-badge" + (hl ? " hl" : unread ? " dot" : "");
  el.textContent = hl ? (hl > 99 ? "99+" : String(hl)) : "";
}

/** After a theme is applied: colour the Android status and navigation bars to match. */
export function afterTheme() {
  if (!android) return;
  const bg = getComputedStyle(document.documentElement).getPropertyValue("--bg").trim();
  const rgb = toRgb(bg);
  if (!rgb) return;
  const lum = (0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2]) / 255;
  window.icbAndroid.setSystemBars(bg, lum > 0.6);
}

function toRgb(c) {
  const probe = document.createElement("span");
  probe.style.color = c;
  document.body.appendChild(probe);
  const m = /rgba?\((\d+),\s*(\d+),\s*(\d+)/.exec(getComputedStyle(probe).color);
  probe.remove();
  return m ? [+m[1], +m[2], +m[3]] : null;
}

// ---- setup ----

export function initMobile(fns) {
  ctx = fns;
  setMode();
  narrow.addEventListener("change", setMode);

  $("navBtn").onclick = () => (document.body.classList.contains("nav-open") ? closeDrawers() : openNav());
  $("scrim").onclick = closeDrawers;

  // the user list: a drawer on phones, the usual toggle on desktop
  const desktopUsers = $("usersBtn").onclick;
  $("usersBtn").onclick = (e) => {
    if (!isMobile()) return desktopUsers?.(e);
    document.body.classList.contains("users-open") ? closeDrawers() : openUsers();
  };
  // picking someone in the list puts their nick in the message box
  $("ulBody").addEventListener("click", (e) => { if (isMobile() && e.target.closest(".ul-user")) closeDrawers(); });

  // nick completion without a Tab key; pointerdown keeps the keyboard open
  const tab = $("tabBtn");
  tab.addEventListener("pointerdown", (e) => e.preventDefault());
  tab.onclick = () => { ctx.tabComplete(); $("input").focus(); };

  // tap the topic to read all of it
  $("hTopic").addEventListener("click", () => { if (isMobile()) $("hTopic").classList.toggle("open"); });

  // a little buzz when a long-press opens a menu
  document.addEventListener("contextmenu", (e) => {
    if (android && isMobile()) setTimeout(() => { if (e.defaultPrevented) window.icbAndroid.vibrate(12); });
  });

  setupSwipes();

  // Android back button: close the top-most thing, or let the app go to the background
  window.__icbBack = () => {
    if (!$("menu").classList.contains("hidden")) { ctx.hideMenu(); return true; }
    const picker = $("emojiPicker");
    // (a position:fixed element has no offsetParent, so check the class the picker toggles)
    if (picker && !picker.classList.contains("hidden")) { picker.classList.add("hidden"); return true; }
    if (!$("overlay").classList.contains("hidden")) { ctx.closeOverlay(); return true; }
    if (drawerOpen()) { closeDrawers(); return true; }
    if (!$("findBar").classList.contains("hidden")) { ctx.closeFind(); return true; }
    if ($("hTopic").classList.contains("open")) { $("hTopic").classList.remove("open"); return true; }
    return false;
  };

  // "Share to IRChatterbox" from another app drops the text into the message box
  on("shareText", ({ text }) => {
    const input = $("input");
    input.value = input.value ? input.value + " " + text : text;
    input.dispatchEvent(new Event("input"));
    input.focus();
  });
}

// Swipe right to open the channel list, left for the user list; swipe a drawer closed.
function setupSwipes() {
  let sx = 0, sy = 0, t0 = 0, target = null;
  document.addEventListener("touchstart", (e) => {
    if (!isMobile() || e.touches.length !== 1) { target = null; return; }
    const t = e.touches[0];
    sx = t.clientX; sy = t.clientY; t0 = Date.now();
    target = e.target;
  }, { passive: true });
  document.addEventListener("touchend", (e) => {
    if (!target || !isMobile()) return;
    const t = e.changedTouches[0];
    const dx = t.clientX - sx, dy = t.clientY - sy;
    const el = target;
    target = null;
    if (Date.now() - t0 > 600 || Math.abs(dx) < 70 || Math.abs(dy) > Math.abs(dx) * 0.6) return;
    if (!$("overlay").classList.contains("hidden")) return;
    if (el.closest("input, textarea, .media, iframe, video, #emojiPicker")) return;
    if (window.getSelection()?.toString()) return; // selecting text, not swiping
    const body = document.body.classList;
    if (body.contains("nav-open")) { if (dx < 0) closeDrawers(); return; }
    if (body.contains("users-open")) { if (dx > 0) closeDrawers(); return; }
    if (!el.closest("#main")) return;
    if (dx > 0) openNav();
    else if (!$("usersBtn").classList.contains("m-hide")) openUsers();
  }, { passive: true });
}
