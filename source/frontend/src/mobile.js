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

// Drag the drawers: swipe right on the chat to pull the channel list out, left for the user
// list, and drag an open drawer back. The drawer follows your finger; on release it opens or
// closes depending on how far you dragged and how fast you flicked.
function setupSwipes() {
  const FLICK = 0.35;     // px/ms: faster than this finishes the move in that direction
  const SLOP = 10;        // px before deciding between a drawer drag and a scroll
  let start = null;       // { x, y, target } of the touch, until it's decided
  let drag = null;        // { el, side: "left"|"right", wasOpen, w, x0, pts: [[t, x]] }

  const blocked = (el) =>
    !$("overlay").classList.contains("hidden") ||
    el.closest("input, textarea, .media, iframe, video, #emojiPicker, #menu") ||
    window.getSelection()?.toString();

  // where the drawer sits for a finger dx from the start: 0 = closed, 1 = open
  const progress = (dx) => {
    const d = drag.side === "left" ? dx : -dx;
    return Math.min(1, Math.max(0, (drag.wasOpen ? 1 : 0) + d / drag.w));
  };
  const paint = (p) => {
    const off = (1 - p) * drag.w * (drag.side === "left" ? -1 : 1);
    drag.el.style.transform = `translateX(${off}px)`;
    $("scrim").style.opacity = String(p);
    $("scrim").style.pointerEvents = "auto";
  };
  const release = (p, v) => {
    const { el, side } = drag;
    const towardOpen = side === "left" ? v : -v; // px/ms in the opening direction
    const open = towardOpen > FLICK || (towardOpen > -FLICK && p > 0.5);
    // a quick flick finishes quickly; a slow drag eases the rest of the way
    const fast = Math.abs(towardOpen) > FLICK;
    const ms = Math.round((fast ? 140 : 220) * (open ? 1 - p : p) + 60);
    el.style.transition = `transform ${ms}ms cubic-bezier(.2,.8,.2,1), box-shadow .22s`;
    $("scrim").style.transition = `opacity ${ms}ms`;
    // drop the drag position and let the classes decide; the transition runs from where the finger left it
    el.style.transform = "";
    $("scrim").style.opacity = "";
    $("scrim").style.pointerEvents = "";
    if (open) (side === "left" ? openNav : openUsers)();
    else closeDrawers();
    setTimeout(() => { el.style.transition = ""; $("scrim").style.transition = ""; }, ms + 30);
    drag = null;
  };

  document.addEventListener("touchstart", (e) => {
    start = drag = null;
    if (!isMobile() || e.touches.length !== 1 || blocked(e.target)) return;
    const t = e.touches[0];
    start = { x: t.clientX, y: t.clientY, target: e.target };
  }, { passive: true });

  document.addEventListener("touchmove", (e) => {
    if (!start && !drag) return;
    const t = e.touches[0];
    if (start) {
      const dx = t.clientX - start.x, dy = t.clientY - start.y;
      if (Math.abs(dx) < SLOP && Math.abs(dy) < SLOP) return;
      const el = start.target;
      start = null;
      if (Math.abs(dy) > Math.abs(dx) * 0.8) return; // scrolling, not a drawer
      const body = document.body.classList;
      let side, wasOpen = true;
      if (body.contains("nav-open")) { if (dx > 0) return; side = "left"; }
      else if (body.contains("users-open")) { if (dx < 0) return; side = "right"; }
      else {
        if (!el.closest("#main")) return;
        wasOpen = false;
        if (dx > 0) side = "left";
        else if (!$("usersBtn").classList.contains("m-hide")) side = "right";
        else return;
      }
      const d = $(side === "left" ? "sidebar" : "userlist");
      drag = { el: d, side, wasOpen, w: d.offsetWidth || 300, x0: t.clientX - dx, pts: [] };
      d.style.transition = "none";
      $("scrim").style.transition = "none";
      if (window.getSelection) window.getSelection().removeAllRanges();
    }
    e.preventDefault(); // the drawer moves, the chat doesn't scroll
    const now = performance.now();
    drag.pts.push([now, t.clientX]);
    while (drag.pts.length > 2 && now - drag.pts[0][0] > 100) drag.pts.shift();
    paint(progress(t.clientX - drag.x0));
  }, { passive: false });

  const end = (e) => {
    start = null;
    if (!drag) return;
    const t = e.changedTouches[0];
    const x = t ? t.clientX : drag.pts.at(-1)?.[1] ?? drag.x0;
    // finger speed over its last ~100ms of movement; a pause before letting go means no flick
    const first = drag.pts[0], last = drag.pts.at(-1);
    let v = 0;
    if (first && last && last[0] > first[0] && performance.now() - last[0] < 80) v = (last[1] - first[1]) / (last[0] - first[0]);
    const p = progress(x - drag.x0);
    release(e.type === "touchcancel" ? (drag.wasOpen ? 1 : 0) : p, v);
  };
  document.addEventListener("touchend", end, { passive: true });
  document.addEventListener("touchcancel", end, { passive: true });
}
