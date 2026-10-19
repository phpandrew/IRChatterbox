import { Go, on, initApi, setTitle } from "./api.js";
import { esc, ircToHtml, nickColor, baseNick, setNickOverrides, setNickContrast, findUrls, mediaFor, mentionRegex, stripCodes } from "./format.js";
import { applyTheme, resolveTheme, setCustomThemes, onSystemThemeChange, systemIsDark } from "./themes.js";
import logoUrl from "./assets/logo.png";
import { toggleEmojiPicker } from "./emoji.js";
import { playSound } from "./sounds.js";
import { openServerEditor, openSettings, openPrompt, openColorPicker, openQuitConfirm, openPasteConfirm, openReportDialog } from "./modals.js";
import { detectOS, os } from "./platform.js";
import { setLanguage, t } from "./i18n.js";
import { initMobile, isMobile, afterSelect, afterHeader, afterTree, afterTheme, android } from "./mobile.js";

const $ = (id) => document.getElementById(id);
const MAX_LINES = 2000;
const RANKS = "~&@%+";
const GROUPS = { "~": "Owners", "&": "Admins", "@": "Operators", "%": "Half-ops", "+": "Voiced", "": "Members" };

export const S = {
  cfg: null,
  servers: [], // ordered
  byId: {},
  active: { server: null, key: null },
  focused: document.hasFocus(),
  inputHistory: [],
  histPos: -1,
  ui: loadUi(),
};

function loadUi() {
  try { return JSON.parse(localStorage.getItem("ui") || "{}"); } catch { return {}; }
}
function saveUi() {
  try { localStorage.setItem("ui", JSON.stringify(S.ui)); } catch {}
}

// ---------- model ----------
function makeServer(cfg) {
  const srv = { id: cfg.id, cfg, status: "disconnected", nick: cfg.nick, network: "", lag: null, buffers: new Map() };
  srv.collapsed = !!S.ui.collapsed?.[cfg.id];
  ensureBuffer(srv, "", "server");
  return srv;
}

function ensureBuffer(srv, name, type) {
  const key = (name || "").toLowerCase();
  let b = srv.buffers.get(key);
  if (!b) {
    b = { key, name: name || "", type: type || (isChannel(srv, name) ? "channel" : "query"), lines: [], unread: 0, hl: 0,
      activity: false, topic: "", modes: "", users: [], joined: false, marker: null, markPending: true, created: Date.now(), history: false };
    srv.buffers.set(key, b);
    if (b.type !== "server") loadHistory(srv, b);
    scheduleTree();
    if (isSplit()) renderPanesSoon();
  }
  return b;
}

function isChannel(srv, name) { return !!name && "#&!+".includes(name[0]); }
function activeServer() { return S.byId[S.active.server]; }
function activeBuffer() { const s = activeServer(); return s?.buffers.get(S.active.key); }

async function loadHistory(srv, b) {
  b.history = true;
  const lines = (await Go.GetHistory(srv.id, b.name)) || [];
  const redacted = new Map(lines.filter((ev) => ev.type === "redact").map((ev) => [ev.text, ev.extra?.reason || ""]));
  const reacts = lines.filter((ev) => ev.type === "react" && ev.time < b.created);
  const old = lines.filter((ev) => ev.time < b.created && ev.type !== "redact" && ev.type !== "react").map((ev) => {
    const l = { ...evToLine(ev), history: true, hl: false };
    if (l.extra?.msgid && redacted.has(l.extra.msgid)) { l.redacted = true; l.redactReason = redacted.get(l.extra.msgid); }
    return l;
  });
  if (!old.length) return;
  b.lines = old.concat(b.lines);
  for (const ev of reacts) applyReact(b, ev);
  if (isActive(srv, b)) renderChat();
  if (onScreen(srv, b)) renderPanesSoon();
}

function isActive(srv, b) { return S.active.server === srv.id && S.active.key === b.key; }

function evToLine(ev) {
  return { t: ev.time, kind: ev.kind || ev.type, type: ev.type, nick: ev.nick, prefix: ev.prefix, text: ev.text || "", self: ev.self, hl: ev.highlight, extra: ev.extra, replay: !!ev.extra?.replay };
}

function resolveBuffer(srv, name) {
  if (name === "*active*") {
    if (S.active.server === srv.id) return srv.buffers.get(S.active.key) || srv.buffers.get("");
    return srv.buffers.get("");
  }
  return ensureBuffer(srv, name);
}

function addLine(srv, b, line) {
  // history from a bouncer can arrive after newer lines: keep the buffer in time order
  const last = b.lines[b.lines.length - 1];
  const outOfOrder = line.replay && last && line.t < last.t;
  if (outOfOrder) {
    let i = b.lines.length;
    while (i > 0 && b.lines[i - 1].t > line.t) i--;
    b.lines.splice(i, 0, line);
  } else b.lines.push(line);
  if (b.lines.length > MAX_LINES) {
    const drop = b.lines.length - MAX_LINES;
    b.lines.splice(0, drop);
  }
  // already read on another device (bouncer read marker)?
  const counts = ["privmsg", "action", "notice"].includes(line.kind) && !line.self && !(b.readMark && line.t <= b.readMark);
  const visible = onScreen(srv, b) && S.focused;
  if (visible && counts) sendReadSoon(srv, b);
  // Marker line: the first message that arrives after you stopped looking at this buffer
  // (switched away or alt-tabbed) gets the divider above it.
  let moveMarker = false;
  if (counts && !visible && b.markPending) {
    b.marker = line;
    b.markPending = false;
    moveMarker = true;
  }
  if (isActive(srv, b)) {
    if (outOfOrder) rerenderSoon();
    else {
      if (moveMarker) $("chat").querySelector(".marker-line")?.remove();
      appendLineDom(b, line);
    }
  }
  appendToPanes(srv, b, line, outOfOrder);
  if (!visible && counts) {
    b.unread++;
    if (line.hl || (b.type === "query" && line.kind !== "notice")) b.hl++;
    b.activity = true;
    scheduleTree();
  } else if (!isActive(srv, b) && line.type === "info" && b.type === "server") {
    b.activity = true;
    scheduleTree();
  }
}

let rerenderTimer = 0;
function rerenderSoon() {
  clearTimeout(rerenderTimer);
  rerenderTimer = setTimeout(() => renderChat(), 60);
}

// ---------- read markers (bouncer) ----------
// Tell the bouncer how far you've read, so your phone or other PC shows the same.
function sendRead(srv, b) {
  if (!srv?.cfg?.bouncer || !b || b.type === "server") return;
  let t = 0;
  for (let i = b.lines.length - 1; i >= 0; i--) if (b.lines[i].type === "msg") { t = b.lines[i].t; break; }
  if (t > (b.readSent || 0) && t > (b.readMark || 0)) {
    b.readSent = t;
    Go.MarkRead(srv.id, b.name, t);
  }
}
const readTimers = new Map();
function sendReadSoon(srv, b) {
  if (!srv?.cfg?.bouncer) return;
  const k = srv.id + "\0" + b.key;
  clearTimeout(readTimers.get(k));
  readTimers.set(k, setTimeout(() => { readTimers.delete(k); if (onScreen(srv, b) && S.focused) sendRead(srv, b); }, 1500));
}
// a read marker arrived (from this or another device): recount what's still unread
function applyReadMark(srv, b, t) {
  b.readMark = Math.max(b.readMark || 0, t);
  if (isActive(srv, b) && S.focused) return;
  let unread = 0, hl = 0;
  for (const l of b.lines) {
    if (l.t <= b.readMark || l.self || !["privmsg", "action", "notice"].includes(l.kind)) continue;
    unread++;
    if (l.hl || (b.type === "query" && l.kind !== "notice")) hl++;
  }
  b.unread = unread; b.hl = hl; b.activity = unread > 0;
  if (!unread) { b.marker = null; b.markPending = true; }
  scheduleTree();
}

// ---------- incoming events ----------
function onIrc(ev) {
  const srv = S.byId[ev.server];
  if (!srv) return;
  const set = S.cfg.settings;
  switch (ev.type) {
    case "status": {
      srv.status = ev.extra.status;
      if (ev.extra.nick) srv.nick = ev.extra.nick;
      if (ev.extra.network) srv.network = ev.extra.network;
      scheduleTree(); renderStatus(); renderMe(); renderHeader();
      break;
    }
    case "lag": srv.lag = ev.extra.ms; renderStatus(); break;
    case "reset": {
      for (const b of srv.buffers.values()) if (b.type === "channel") { b.joined = false; b.users = []; }
      scheduleTree(); renderUsers(); renderHeader();
      break;
    }
    case "msg": {
      const b = resolveBuffer(srv, ev.buffer);
      if (b.typing?.has(ev.nick)) setTyping(srv, b, ev.nick, "done");
      if (b.readMark === undefined && srv.readMarks?.[b.key]) b.readMark = srv.readMarks[b.key];
      addLine(srv, b, evToLine(ev));
      break;
    }
    case "typing": {
      const b = srv.buffers.get((ev.buffer || "").toLowerCase());
      if (b) setTyping(srv, b, ev.nick, ev.text);
      break;
    }
    case "redact": {
      const b = srv.buffers.get((ev.buffer || "").toLowerCase());
      const line = b?.lines.find((l) => l.extra?.msgid === ev.text);
      if (line) {
        line.redacted = true;
        line.redactReason = ev.extra?.reason || "";
        if (isActive(srv, b)) renderChat();
        if (onScreen(srv, b)) renderPanesSoon();
      }
      break;
    }
    case "react": {
      const b = srv.buffers.get((ev.buffer || "").toLowerCase());
      const line = b && applyReact(b, ev);
      if (line) refreshLine(srv, b, line);
      break;
    }
    case "read": {
      const key = (ev.buffer || "").toLowerCase();
      (srv.readMarks ||= {})[key] = Math.max(srv.readMarks[key] || 0, ev.time);
      const b = srv.buffers.get(key);
      if (b) applyReadMark(srv, b, ev.time);
      break;
    }
    case "info": {
      const b = resolveBuffer(srv, ev.buffer || "");
      addLine(srv, b, evToLine(ev));
      break;
    }
    case "event": {
      const b = ensureBuffer(srv, ev.buffer);
      if (ev.kind === "join" && ev.self) b.joined = true;
      if (!set.showJoinsParts && ["join", "part", "quit"].includes(ev.kind) && !ev.self) break;
      addLine(srv, b, evToLine(ev));
      break;
    }
    case "buffer": {
      const act = ev.extra?.action;
      if (act === "open") {
        const b = ensureBuffer(srv, ev.buffer, ev.kind);
        if (ev.kind === "channel") b.joined = true;
        if (ev.kind === "query") Go.OpenQuery(srv.id, ev.buffer);
        if (ev.extra.focus) select(srv.id, b.key);
      } else if (act === "parted") {
        const b = srv.buffers.get(ev.buffer.toLowerCase());
        if (b) { b.joined = false; b.users = []; }
        if (b && isActive(srv, b)) renderUsers();
      } else if (act === "close") {
        closeBufferLocal(srv, ev.buffer.toLowerCase());
      }
      scheduleTree(); renderHeader();
      break;
    }
    case "users": {
      const b = ensureBuffer(srv, ev.buffer, "channel");
      b.users = ev.extra.users || [];
      b.joined = true;
      if (isActive(srv, b)) { renderUsers(); renderHeader(); }
      break;
    }
    case "topic": {
      const b = ensureBuffer(srv, ev.buffer, "channel");
      b.topic = ev.text || "";
      if (isActive(srv, b)) renderHeader();
      break;
    }
    case "modes": {
      const b = ensureBuffer(srv, ev.buffer, "channel");
      b.modes = ev.text || "";
      if (isActive(srv, b)) renderHeader();
      break;
    }
    case "rename": {
      if (ev.self) { srv.nick = ev.text; renderMe(); }
      const old = srv.buffers.get(ev.nick.toLowerCase());
      if (old && old.type === "query" && !srv.buffers.has(ev.text.toLowerCase())) {
        srv.buffers.delete(old.key);
        old.key = ev.text.toLowerCase(); old.name = ev.text;
        srv.buffers.set(old.key, old);
        addLine(srv, old, { t: ev.time, kind: "nick", type: "event", nick: ev.nick, text: ev.text });
        if (S.active.server === srv.id && S.active.key === ev.nick.toLowerCase()) S.active.key = old.key;
        scheduleTree(); renderHeader();
      }
      break;
    }
  }
}

function closeBufferLocal(srv, key) {
  if (!key) return;
  srv.buffers.delete(key);
  if (isSplit()) paneList().forEach((p, i) => { if (p && p.server === srv.id && p.key === key && i !== S.ui.focus) S.ui.panes[i] = null; });
  renderPanesSoon();
  if (S.active.server === srv.id && S.active.key === key) select(srv.id, "");
  scheduleTree();
}

// ---------- selection ----------
function select(serverId, key) {
  if (isSplit()) {
    const list = paneList();
    // already open in another pane: focus that pane instead of showing it twice
    const j = list.findIndex((p, i) => i !== S.ui.focus && p && p.server === serverId && p.key === key);
    if (j >= 0) { S.ui.focus = j; buildPanes(); }
    list[S.ui.focus] = { server: serverId, key };
    saveUi();
  }
  const prev = activeBuffer();
  const switching = !(S.active.server === serverId && S.active.key === key);
  if (prev && switching && inputFor === draftKey(activeServer(), prev)) saveDraft(activeServer(), prev);
  if (prev && switching) {
    // leaving a buffer: everything in it now counts as read, so the next message gets the line
    prev.marker = null;
    prev.markPending = true;
  }
  S.active = { server: serverId, key };
  const srv = activeServer();
  const b = activeBuffer();
  if (!srv || !b) return;
  S.ui.last = { server: serverId, name: b.name };
  saveUi();
  Go.SetActive(serverId, b.name);
  renderAll();
  renderTyping();
  renderReplyBar();
  if (inputFor !== draftKey(srv, b)) restoreDraft(srv, b);
  b.markPending = false; // you're reading it now
  b.unread = 0; b.hl = 0; b.activity = false;
  scheduleTree();
  if (S.focused) sendRead(srv, b);
  afterSelect();
  if (!isMobile()) $("input").focus(); // on a phone that would pop the keyboard up every time
}

// ---------- drafts: what you'd typed in each channel stays there when you switch ----------
const DRAFTS_KEY = "drafts";
let drafts = (() => { try { return JSON.parse(localStorage.getItem(DRAFTS_KEY) || "{}"); } catch { return {}; } })();
let draftTimer = 0;
function draftKey(srv, b) { return srv.id + "/" + b.key; }
function persistDrafts() {
  clearTimeout(draftTimer);
  draftTimer = setTimeout(() => { try { localStorage.setItem(DRAFTS_KEY, JSON.stringify(drafts)); } catch {} }, 400);
}
window.addEventListener("beforeunload", () => { clearTimeout(draftTimer); try { localStorage.setItem(DRAFTS_KEY, JSON.stringify(drafts)); } catch {} });
function saveDraft(srv, b) {
  if (!srv || !b) return;
  const v = $("input").value;
  const k = draftKey(srv, b);
  if (v.trim()) drafts[k] = v.slice(0, 20000); else delete drafts[k];
  persistDrafts();
}
let inputFor = ""; // whose draft the message box holds
function restoreDraft(srv, b) {
  inputFor = draftKey(srv, b);
  const inp = $("input");
  inp.value = drafts[draftKey(srv, b)] || "";
  autosize();
  inp.setSelectionRange(inp.value.length, inp.value.length);
}
function hasDraft(srv, b) { return !!drafts[draftKey(srv, b)] && !isActive(srv, b); }

function allBuffers() {
  const out = favourites().filter((f) => f.b).map(({ srv, b }) => ({ srv, b }));
  for (const srv of S.servers) {
    for (const b of sortedBuffers(srv)) if (!isFavourite(srv, b)) out.push({ srv, b });
  }
  return out;
}

// ---------- favourites: channels and chats pinned to the top of the sidebar ----------
// [{ srv, key, name, b }] in the saved order; b is the buffer once it exists
function favourites() {
  const out = [];
  for (const f of S.cfg?.favourites || []) {
    const srv = S.byId[f.server];
    if (!srv || !f.buffer) continue;
    const key = f.buffer.toLowerCase();
    out.push({ srv, key, name: f.buffer, b: srv.buffers.get(key) });
  }
  return out;
}
function isFavourite(srv, b) {
  return (S.cfg?.favourites || []).some((f) => f.server === srv.id && (f.buffer || "").toLowerCase() === b.key);
}
function toggleFavourite(srv, b) {
  const on = !isFavourite(srv, b);
  Go.SetFavourite(srv.id, b.name, on);
  toast(on ? t("{0} added to Favourites", b.name) : t("{0} removed from Favourites", b.name), "ok");
}

function sortedBuffers(srv) {
  const list = [...srv.buffers.values()];
  const rank = (b) => (b.type === "server" ? 0 : b.type === "channel" ? 1 : 2);
  const order = srv.cfg.channelOrder || [];
  const pos = (b) => { const i = order.indexOf(b.key); return i < 0 ? 1e6 : i; };
  return list.sort((a, b) => rank(a) - rank(b) || (a.type === "channel" ? pos(a) - pos(b) : 0) || a.key.localeCompare(b.key));
}

function cycleBuffer(dir, unreadOnly) {
  const list = allBuffers().filter(({ srv }) => !srv.collapsed || true);
  if (!list.length) return;
  let i = list.findIndex(({ srv, b }) => isActive(srv, b));
  for (let n = 0; n < list.length; n++) {
    i = (i + dir + list.length) % list.length;
    const { srv, b } = list[i];
    if (!unreadOnly || b.unread > 0) return select(srv.id, b.key);
  }
}

// ---------- rendering: sidebar ----------
let treeQueued = false;
function scheduleTree() {
  if (treeQueued) return;
  treeQueued = true;
  requestAnimationFrame(() => { treeQueued = false; renderTree(); });
}

const ICONS = {
  server: `<svg viewBox="0 0 24 24"><rect x="4" y="3.5" width="16" height="17" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></svg>`,
  query: `<svg viewBox="0 0 24 24"><circle cx="12" cy="8.5" r="3.5"/><path d="M5 20c.8-3.8 3.6-5.6 7-5.6s6.2 1.8 7 5.6"/></svg>`,
  chev: `<svg class="chev" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg>`,
  smile: `<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="8.5"/><path d="M8.7 14c.8 1.1 1.9 1.7 3.3 1.7s2.5-.6 3.3-1.7"/><path d="M9.2 9.8h.01M14.8 9.8h.01" stroke-width="2.4"/></svg>`,
  reply: `<svg viewBox="0 0 24 24"><path d="M10 8 5 12.5l5 4.5"/><path d="M5.5 12.5H14a5 5 0 0 1 5 5V19"/></svg>`,
  replyTo: `<svg viewBox="0 0 24 24"><path d="M6 5v6a4 4 0 0 0 4 4h9"/><path d="m15 11 4 4-4 4"/></svg>`,
  bellOff: `<svg viewBox="0 0 24 24"><path d="M9.5 20a2.6 2.6 0 0 0 5 0"/><path d="M17.5 13.5V10a5.5 5.5 0 0 0-9.3-4M6.5 9.8V14L5 16.5h11.5"/><path d="m4 4 16 16"/></svg>`,
  pencil: `<svg viewBox="0 0 24 24"><path d="M4 20h4L19 9l-4-4L4 16z"/><path d="m13.5 6.5 4 4"/></svg>`,
  star: `<svg viewBox="0 0 24 24"><path d="m12 3.8 2.5 5.1 5.6.8-4 3.9.9 5.6-5-2.6-5 2.6.9-5.6-4-3.9 5.6-.8z"/></svg>`,
  bell: `<svg viewBox="0 0 24 24"><path d="M9.5 20a2.6 2.6 0 0 0 5 0"/><path d="M6.5 10a5.5 5.5 0 0 1 11 0v4l1.5 2.5H5L6.5 14z"/></svg>`,
};

// ---------- channel icons and the compact sidebar ----------
S.icons = {};
function chanIcon(srv, b) {
  const u = !b ? "" : b.type === "server" ? S.icons[srv.id + "/*"] : S.icons[srv.id + "/" + b.key];
  return u && /^data:image\/(png|webp|jpeg);base64,/.test(u) ? u : "";
}
// Shrink a picked picture to a 128×128 square (cropped to the middle) and save it as the icon.
function pickChannelIcon(srv, b) { pickIcon(srv.id, b.name, b.name); }
// Your own avatar is stored the same way, as "_me/avatar". IRC can't send it to others.
function myAvatar() {
  const u = S.icons["_me/avatar"];
  return u && /^data:image\/(png|webp|jpeg);base64,/.test(u) ? u : "";
}
function pickIcon(server, name, label) {
  const inp = document.createElement("input");
  inp.type = "file";
  inp.accept = "image/png,image/jpeg,image/webp,image/gif,image/bmp";
  inp.onchange = async () => {
    const f = inp.files?.[0];
    if (!f) return;
    try {
      const img = await createImageBitmap(f);
      const side = Math.min(img.width, img.height), c = document.createElement("canvas");
      c.width = c.height = 128;
      const ctx = c.getContext("2d");
      ctx.imageSmoothingQuality = "high";
      ctx.drawImage(img, (img.width - side) / 2, (img.height - side) / 2, side, side, 0, 0, 128, 128);
      const err = await Go.SetChannelIcon(server, name, c.toDataURL("image/png"));
      if (err) toast(err, "error"); else toast(server === "_me" ? "Avatar updated" : `Icon set for ${label}`, "ok");
    } catch { toast("That picture couldn't be opened", "error"); }
  };
  inp.click();
}
function setSidebarMini(on) {
  S.ui.sbMini = !!on;
  saveUi();
  document.body.classList.toggle("sb-mini", S.ui.sbMini);
  const t = $("sbToggle");
  if (t) t.title = S.ui.sbMini ? "Expand sidebar" : "Collapse sidebar";
}

// The server's own icon (Edit server → Advanced, or its right-click menu), or its first letter.
function srvIcon(srv) {
  const u = S.icons[srv.id + "/*"];
  const ok = u && /^data:image\/(png|webp|jpeg);base64,/.test(u);
  const letter = esc(((srv.cfg.name || srv.cfg.host || "?")[0] || "?").toUpperCase());
  return `<span class="srv-ico${ok ? " pic" : ""}" style="--tile:${nickColor(srv.cfg.name || srv.cfg.host || "")}">${ok ? `<img src="${u}" alt="">` : letter}</span>`;
}

function renderTree() {
  if (drag) { treeDirty = true; return; } // don't rebuild the list mid-drag
  const tree = $("tree");
  let html = "";
  // favourites first: make sure each has a buffer (a channel you're not in shows as parted)
  const favs = favourites();
  for (const f of favs) if (!f.b) f.b = ensureBuffer(f.srv, f.name);
  if (favs.length) {
    let fU = 0, fH = 0, rows = "";
    const many = S.servers.length > 1;
    for (const { srv, b } of favs) {
      fU += b.unread; fH += b.hl;
      rows += bufRow(srv, b, many ? srv.cfg.name || srv.cfg.host : "");
    }
    const fc = !!S.ui.favCollapsed;
    const agg = fc ? (fH ? `<span class="badge hl">${fH}</span>` : fU ? `<span class="badge">${fU}</span>` : "") : "";
    html += `<div class="srv fav-sec ${fc ? "collapsed" : ""}">
      <div class="srv-head fav-head" data-favhead="1">${ICONS.chev}<span class="fav-star">${ICONS.star}</span>
        <div style="flex:1;min-width:0"><div class="fav-title">Favourites</div></div>${agg}</div>
      <div class="srv-kids">${rows}</div></div>`;
  }
  for (const srv of S.servers) {
    let aggU = 0, aggH = 0;
    let kids = "";
    for (const b of sortedBuffers(srv)) {
      if (isFavourite(srv, b)) continue;
      aggU += b.unread; aggH += b.hl;
      kids += bufRow(srv, b);
    }
    const stTxt = { connected: "Connected", connecting: "Connecting…", disconnected: "Disconnected" }[srv.status];
    const agg = srv.collapsed ? (aggH ? `<span class="badge hl">${aggH}</span>` : aggU ? `<span class="badge">${aggU}</span>` : "") : "";
    html += `<div class="srv ${srv.collapsed ? "collapsed" : ""}" data-srv="${srv.id}">
      <div class="srv-head" data-head="${srv.id}" draggable="true">${ICONS.chev}
        ${srvIcon(srv)}<div style="flex:1;min-width:0"><div class="srv-name">${esc(srv.cfg.name || srv.cfg.host)}</div>
        <div class="srv-state"><i class="dot ${srv.status}"></i>${stTxt}${srv.status === "connected" && srv.nick ? " as " + esc(srv.nick) : ""}</div></div>${agg}
      </div>
      <div class="srv-kids">${kids}</div></div>`;
  }
  if (!S.servers.length) html = `<div class="empty" style="padding:30px 10px">No servers yet.</div>`;
  tree.innerHTML = html;
  updateTitle();
  afterTree(S.servers, S.active);
}

// ---------- per-channel notifications ----------
function notifyRule(srv, b) {
  const r = S.cfg?.channelNotify?.[srv.id + "/" + b.key];
  if (!r || (r.level === "mute" && r.until && Date.now() >= r.until)) return null;
  return r;
}
function notifyLevel(srv, b) {
  return notifyRule(srv, b)?.level || (b.type === "channel" ? "mentions" : "all");
}
function notifyMenu(srv, b) {
  const r = notifyRule(srv, b), level = notifyLevel(srv, b);
  const set = (lvl, minutes = 0) => () => Go.SetChannelNotify(srv.id, b.name, lvl, minutes);
  const items = [["Every message", set("all"), "", level === "all"]];
  if (b.type === "channel") items.push(["Only mentions", set("mentions"), "", level === "mentions"]);
  items.push(["Nothing", set("mute"), "", level === "mute" && !r?.until]);
  items.push("-");
  items.push(["Mute for 1 hour", set("mute", 60)]);
  items.push(["Mute for 8 hours", set("mute", 480)]);
  items.push(["Mute for 24 hours", set("mute", 1440)]);
  if (level === "mute") {
    items.push("-");
    if (r?.until) items.push([t("Muted until {0}", fmtWhen(r.until)), () => {}, "muted-note"]);
    items.push(["Unmute", set("")]);
  }
  return items;
}
function fmtWhen(ms) {
  const d = new Date(ms), now = new Date();
  return d.toDateString() === now.toDateString() ? fmtTime(ms) : d.toLocaleDateString(undefined, { weekday: "short" }) + " " + fmtTime(ms);
}
setInterval(() => { // mutes that run out
  if (Object.values(S.cfg?.channelNotify || {}).some((r) => r.until && r.until <= Date.now() && r.until > Date.now() - 61000)) scheduleTree();
}, 60000);

// one sidebar row; fav = the server's name to show beside a favourite ("" = none), or undefined
function bufRow(srv, b, fav) {
  const active = isActive(srv, b);
  const muted = notifyLevel(srv, b) === "mute";
  const isFav = fav !== undefined;
  const cls = ["buf", active && "active", b.activity && "activity", b.hl && "hl", b.type === "channel" && !b.joined && "parted", muted && "muted", isFav && "fav"].filter(Boolean).join(" ");
  const ico = b.type === "server" ? ICONS.server : b.type === "channel" ? "#" : ICONS.query;
  const label = b.type === "server" ? "Server status" : b.type === "channel" ? esc(b.name.replace(/^#/, "")) : esc(b.name);
  const badge = b.hl ? `<span class="badge hl">${b.hl}</span>` : b.unread ? `<span class="badge">${b.unread > 999 ? "999+" : b.unread}</span>` : "";
  const x = b.type !== "server" ? `<span class="x" data-close="1" title="${b.type === "channel" ? "Leave" : "Close"}">✕</span>` : "";
  const pic = chanIcon(srv, b);
  const letter = esc((b.type === "server" ? (srv.cfg.name || "?") : b.name.replace(/^[#&!+]+/, "")).charAt(0).toUpperCase() || "#");
  const icoHtml = pic ? `<img src="${pic}" alt="">` : ico;
  const drag = isFav || b.type === "channel" ? ` draggable="true"` : "";
  const tip = b.type === "server" ? "Server status" : fav ? `${b.name} · ${fav}` : b.name;
  const where = fav ? `<span class="fav-srv">${esc(fav)}</span>` : "";
  return `<div class="${cls}" data-s="${srv.id}" data-k="${esc(b.key)}"${isFav ? ` data-fav="1"` : ""}${drag} title="${esc(tip)}"><span class="ico${pic ? " pic" : ""}" data-l="${letter}" style="--tile:${nickColor(b.name || srv.cfg.name || "")}">${icoHtml}</span><span class="nm">${label}${where}</span>${hasDraft(srv, b) ? `<span class="draft-ico" title="${esc(t("Unsent message"))}">${ICONS.pencil}</span>` : ""}${muted ? `<span class="mute-ico" title="${esc(t("Notifications muted"))}">${ICONS.bellOff}</span>` : ""}${badge}${x}</div>`;
}

function updateTitle() {
  const srv = activeServer(), b = activeBuffer();
  let total = 0;
  for (const s of S.servers) for (const x of s.buffers.values()) total += x.hl;
  const where = b ? (b.type === "server" ? srv.cfg.name : `${b.name} · ${srv.network || srv.cfg.name}`) : "";
  setTitle(`${total ? `(${total}) ` : ""}IRChatterbox${where ? " — " + where : ""}`);
}

// ---------- drag to reorder servers and channels ----------
let drag = null, treeDirty = false;
function clearDropMarks() {
  document.querySelectorAll(".drop-before,.drop-after").forEach((el) => el.classList.remove("drop-before", "drop-after"));
}
$("tree").addEventListener("dragstart", (e) => {
  const head = e.target.closest?.(".srv-head[data-head]");
  const row = e.target.closest?.(".buf[draggable]");
  if (head) drag = { kind: "server", id: head.dataset.head, el: head.closest(".srv") };
  else if (row?.dataset.fav) drag = { kind: "fav", srv: row.dataset.s, key: row.dataset.k, el: row };
  else if (row) drag = { kind: "channel", srv: row.dataset.s, key: row.dataset.k, el: row };
  else return;
  e.dataTransfer.effectAllowed = "move";
  e.dataTransfer.setData("text/plain", drag.kind);
  requestAnimationFrame(() => drag?.el.classList.add("dragging"));
});
function dropTarget(e) {
  if (!drag) return null;
  if (drag.kind === "server") {
    const srvEl = e.target.closest(".srv:not(.fav-sec)");
    return srvEl && srvEl !== drag.el ? srvEl : null;
  }
  if (drag.kind === "fav") {
    const row = e.target.closest(".buf[data-fav]");
    return row && row !== drag.el ? row : null;
  }
  const row = e.target.closest(".buf[draggable]:not([data-fav])");
  return row && row !== drag.el && row.dataset.s === drag.srv ? row : null;
}
$("tree").addEventListener("dragover", (e) => {
  const t = dropTarget(e);
  clearDropMarks();
  if (!t) return;
  e.preventDefault();
  const r = t.getBoundingClientRect();
  t.classList.add(e.clientY < r.top + r.height / 2 ? "drop-before" : "drop-after");
});
$("tree").addEventListener("drop", (e) => {
  const t = dropTarget(e);
  if (!t) return;
  e.preventDefault();
  const after = t.classList.contains("drop-after");
  if (drag.kind === "fav") {
    const list = favourites().map((f) => ({ server: f.srv.id, buffer: f.name }));
    const at = (s, k) => list.findIndex((f) => f.server === s && f.buffer.toLowerCase() === k);
    const moved = list.splice(at(drag.srv, drag.key), 1)[0];
    list.splice(at(t.dataset.s, t.dataset.k) + (after ? 1 : 0), 0, moved);
    S.cfg.favourites = list;
    Go.SetFavouriteOrder(list);
  } else if (drag.kind === "server") {
    const from = S.servers.findIndex((s) => s.id === drag.id);
    const moved = S.servers.splice(from, 1)[0];
    let to = S.servers.findIndex((s) => s.id === t.dataset.srv) + (after ? 1 : 0);
    S.servers.splice(to, 0, moved);
    Go.ReorderServers(S.servers.map((s) => s.id));
  } else {
    const srv = S.byId[drag.srv];
    const names = sortedBuffers(srv).filter((b) => b.type === "channel").map((b) => b.key);
    names.splice(names.indexOf(drag.key), 1);
    names.splice(names.indexOf(t.dataset.k) + (after ? 1 : 0), 0, drag.key);
    srv.cfg.channelOrder = names;
    Go.SetChannelOrder(srv.id, names);
  }
});
$("tree").addEventListener("dragend", () => {
  drag?.el.classList.remove("dragging");
  drag = null;
  clearDropMarks();
  renderTree();
});

$("tree").addEventListener("click", (e) => {
  if (e.target.closest("[data-favhead]")) {
    S.ui.favCollapsed = !S.ui.favCollapsed;
    saveUi();
    renderTree();
    return;
  }
  const head = e.target.closest("[data-head]");
  if (head) {
    const srv = S.byId[head.dataset.head];
    srv.collapsed = !srv.collapsed;
    S.ui.collapsed = { ...(S.ui.collapsed || {}), [srv.id]: srv.collapsed };
    saveUi();
    renderTree();
    return;
  }
  const row = e.target.closest(".buf");
  if (!row) return;
  const srv = S.byId[row.dataset.s];
  if (e.target.closest("[data-close]")) {
    const b = srv.buffers.get(row.dataset.k);
    closeBuffer(srv, b);
    return;
  }
  select(row.dataset.s, row.dataset.k);
  const b = srv.buffers.get(row.dataset.k);
  if (row.dataset.fav && b?.type === "channel" && !b.joined && srv.status === "connected") Go.SendInput(srv.id, b.name, "/join " + b.name);
});

function closeBuffer(srv, b) {
  // a closed private chat leaves Favourites too (a channel stays there, greyed, to rejoin)
  if (b.type !== "channel" && isFavourite(srv, b)) Go.SetFavourite(srv.id, b.name, false);
  Go.CloseBuffer(srv.id, b.name);
  closeBufferLocal(srv, b.key);
}

$("tree").addEventListener("contextmenu", (e) => {
  e.preventDefault();
  const head = e.target.closest("[data-head]");
  const row = e.target.closest(".buf");
  if (e.target.closest("[data-favhead]")) return;
  if (head) return serverMenu(S.byId[head.dataset.head], e);
  if (row) {
    const srv = S.byId[row.dataset.s];
    const b = srv.buffers.get(row.dataset.k);
    if (b.type === "server") return serverMenu(srv, e);
    const items = [];
    items.push([isFavourite(srv, b) ? "Remove from Favourites" : "Add to Favourites", () => toggleFavourite(srv, b)]);
    if (b.type === "channel") {
      items.push(b.joined ? ["Leave channel", () => Go.SendInput(srv.id, b.name, "/part " + b.name)] : ["Rejoin", () => Go.SendInput(srv.id, b.name, "/join " + b.name)]);
    } else {
      items.push(["Whois", () => Go.SendInput(srv.id, b.name, "/whois " + b.name)]);
    }
    if (b.type === "channel") {
      const auto = (srv.cfg.autoJoin || []).some((a) => (a.split(/\s+/)[0] || "").toLowerCase() === b.key);
      items.push(["Join on connect", async () => {
        await Go.SetAutoJoin(srv.id, b.name, !auto);
        await reloadConfig();
        toast(!auto ? `${b.name} will join automatically` : `${b.name} won't join automatically`, "ok");
      }, "", auto]);
    }
    const sv = S.cfg.startView || {};
    const isStart = sv.server === srv.id && (sv.buffer || "").toLowerCase() === b.key;
    items.push(["Show at startup", async () => {
      await Go.SetStartView(isStart ? "" : srv.id, isStart ? "" : b.name);
      S.cfg.startView = isStart ? {} : { server: srv.id, buffer: b.name };
      toast(isStart ? "IRChatterbox will open where you left off" : `IRChatterbox will open on ${b.name}`, "ok");
    }, "", isStart]);
    items.push(["Notifications", notifyMenu(srv, b)]);
    items.push(["Show in split view", () => showInSplit(srv, b)]);
    items.push([chanIcon(srv, b) ? "Change icon…" : "Set icon…", () => pickChannelIcon(srv, b)]);
    if (chanIcon(srv, b)) items.push(["Remove icon", () => Go.SetChannelIcon(srv.id, b.name, "")]);
    items.push("-");
    items.push(["Mark as read", () => { b.unread = b.hl = 0; b.activity = false; scheduleTree(); sendRead(srv, b); }]);
    items.push(["Clear buffer", () => { b.lines = []; if (isActive(srv, b)) renderChat(); }]);
    items.push("-");
    items.push([b.type === "channel" ? "Leave & close" : "Close", () => closeBuffer(srv, b), "danger"]);
    const x = e.clientX, y = e.clientY;
    withPluginItems(items, { server: srv.id, buffer: b.name, kind: b.type === "channel" ? "channel" : "query", nick: b.type === "channel" ? "" : b.name })
      .then((all) => showMenu(x, y, all, b.name));
  }
});

function serverMenu(srv, e) {
  const items = [];
  if (srv.status === "disconnected") items.push(["Connect", () => Go.Connect(srv.id)]);
  else items.push(["Disconnect", () => Go.Disconnect(srv.id)]);
  items.push(["Reconnect", () => Go.SendInput(srv.id, "", "/reconnect")]);
  items.push(["Join channel…", () => openPrompt("Join channel", "#channel", "", (v) => v && Go.SendInput(srv.id, "", "/join " + v))]);
  if (srv.status === "connected") items.push(["Channel list…", () => openChannelList(srv)]);
  items.push("-");
  items.push(["Edit server…", () => editServer(srv.cfg)]);
  const hasIcon = !!S.icons[srv.id + "/*"];
  items.push([hasIcon ? "Change server icon…" : "Set server icon…", () => pickIcon(srv.id, "*", srv.cfg.name || srv.cfg.host)]);
  if (hasIcon) items.push(["Remove server icon", () => Go.SetChannelIcon(srv.id, "*", "")]);
  items.push(["Move up", () => moveServer(srv, -1)], ["Move down", () => moveServer(srv, 1)]);
  items.push("-");
  items.push(["Remove server", () => {
    openPrompt(`Remove ${srv.cfg.name}?`, "", null, async () => {
      await Go.DeleteServer(srv.id);
      S.servers = S.servers.filter((x) => x !== srv);
      delete S.byId[srv.id];
      if (S.active.server === srv.id) S.active = { server: null, key: null };
      renderAll();
    }, "Remove");
  }, "danger"]);
  const x = e.clientX, y = e.clientY;
  withPluginItems(items, { server: srv.id, buffer: "", kind: "status", nick: "" }).then((all) => showMenu(x, y, all, srv.cfg.name));
}

async function moveServer(srv, d) {
  await Go.MoveServer(srv.id, d);
  const i = S.servers.indexOf(srv), j = i + d;
  if (j < 0 || j >= S.servers.length) return;
  [S.servers[i], S.servers[j]] = [S.servers[j], S.servers[i]];
  renderTree();
}

// ---------- rendering: header / status / me ----------
function renderHeader() {
  const srv = activeServer(), b = activeBuffer();
  const name = $("hName"), sub = $("hSub"), topic = $("hTopic"), meta = $("hMeta");
  afterHeader(b);
  if (!srv || !b) { name.textContent = ""; sub.textContent = ""; topic.innerHTML = ""; meta.innerHTML = ""; return; }
  const net = srv.network || srv.cfg.name;
  if (b.type === "channel") {
    name.innerHTML = (chanIcon(srv, b) ? `<img class="h-ico" src="${chanIcon(srv, b)}" alt="">` : `<span class="hash">#</span>`) + esc(b.name.replace(/^#/, ""));
    sub.textContent = `${net}`;
    topic.innerHTML = b.topic ? ircToHtml(b.topic, ctxFor(srv)) : `<span class="muted">No topic set</span>`;
    topic.title = (b.topic ? stripCodes(b.topic) + "\n\n" : "") + "Double-click to change the topic";
    topic.classList.add("editable");
    meta.innerHTML = [
      b.joined ? `<span><i class="dot connected"></i> <b>Joined</b></span>` : `<span><i class="dot"></i> Not joined</span>`,
      b.users.length ? `<span><b>${b.users.length}</b> users</span>` : "",
      b.modes ? `<span><b>${esc(b.modes)}</b></span>` : "",
      srv.cfg.tls ? `<span title="TLS">🔒 <b>TLS</b></span>` : "",
    ].join("");
  } else if (b.type === "query") {
    topic.classList.remove("editable");
    name.innerHTML = (chanIcon(srv, b) ? `<img class="h-ico" src="${chanIcon(srv, b)}" alt="">` : "") + `<span style="color:${nickColor(b.name)}">${esc(b.name)}</span>`;
    sub.textContent = `Private conversation · ${net}`;
    topic.innerHTML = "";
    meta.innerHTML = "";
  } else {
    name.innerHTML = (chanIcon(srv, b) ? `<img class="h-ico" src="${chanIcon(srv, b)}" alt="">` : "") + esc(srv.cfg.name || srv.cfg.host);
    sub.textContent = `${srv.cfg.host}:${srv.cfg.port}${srv.cfg.tls ? " · TLS" : ""}`;
    topic.classList.remove("editable");
    topic.title = "";
    topic.innerHTML = `<span class="muted">Server messages, MOTD and notices</span>`;
    meta.innerHTML = `<span><i class="dot ${srv.status}"></i> <b>${srv.status}</b></span>`;
  }
  // one line only: a long nick or channel name wrapping the placeholder would leave the box
  // two lines tall, even after switching to a buffer with a short name
  const ph = b.type === "server" ? `Type a /command for ${srv.cfg.name}` : `Message ${b.name}`;
  const maxPh = isMobile() ? 24 : 60;
  $("input").placeholder = ph.length > maxPh ? ph.slice(0, maxPh - 1) + "…" : ph;
  if (!$("input").value) autosize();
  const showUsers = b.type === "channel" && S.ui.users !== false;
  $("userlist").classList.toggle("off", !showUsers);
  $("userResizer").classList.toggle("off", !showUsers);
  $("usersBtn").classList.toggle("on", showUsers);
  updateTitle();
}

function renderStatus() {
  const srv = activeServer();
  const c = $("sbConn");
  if (!srv) { c.innerHTML = `<i class="dot"></i><span>Not connected</span>`; $("sbLag").textContent = ""; return; }
  const txt = srv.status === "connected" ? `Connected to ${esc(srv.cfg.host)}:${srv.cfg.port}` : srv.status === "connecting" ? `Connecting to ${esc(srv.cfg.host)}…` : `Disconnected from ${esc(srv.cfg.host)}`;
  c.innerHTML = `<i class="dot ${srv.status}"></i><span>${txt}</span>`;
  $("sbLag").textContent = srv.lag != null && srv.status === "connected" ? `Latency ${srv.lag} ms` : "";
  const b = activeBuffer();
  $("sbModes").textContent = b?.type === "channel" && srv.nick ? `${myPrefix(b, srv.nick)}${srv.nick} · ${b.users.length} users` : srv.nick || "";
}

function myPrefix(b, nick) {
  const u = b.users.find((u) => u.nick.toLowerCase() === nick.toLowerCase());
  return u?.prefix?.[0] || "";
}

function renderMe() {
  const srv = activeServer() || S.servers[0];
  const nick = srv?.nick || srv?.cfg.nick || "—";
  $("meNick").textContent = nick;
  $("meNick").style.color = nickColor(nick);
  if (myAvatar()) {
    $("meAvatar").innerHTML = `<img src="${myAvatar()}" alt="">`;
  } else {
    $("meAvatar").textContent = (nick[0] || "?").toUpperCase();
  }
  $("meAvatar").style.background = nickColor(nick);
  $("meStatus").textContent = srv ? ({ connected: "Online", connecting: "Connecting…", disconnected: "Offline" }[srv.status]) : "Offline";
}

setInterval(() => {
  const d = new Date();
  $("sbClock").textContent = fmtTime(d.getTime()) + "  ·  " + d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}, 1000);

// ---------- rendering: chat ----------
function fmtTime(t) {
  const set = S.cfg?.settings || {};
  const d = new Date(t);
  return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: set.showSeconds ? "2-digit" : undefined, hour12: set.timeFormat === "12h" });
}
function dayLabel(t) {
  const d = new Date(t), now = new Date();
  const y = new Date(now); y.setDate(now.getDate() - 1);
  const same = (a, b) => a.toDateString() === b.toDateString();
  const date = d.toLocaleDateString(undefined, { month: "long", day: "numeric", year: d.getFullYear() === now.getFullYear() ? undefined : "numeric" });
  return same(d, now) ? `Today · ${date}` : same(d, y) ? `Yesterday · ${date}` : d.toLocaleDateString(undefined, { weekday: "long" }) + " · " + date;
}

function ctxFor(srv) {
  const words = [srv.nick, ...(S.cfg.settings.highlightWords || [])];
  return { mentionRe: mentionRegex(words) };
}

const EV_SYMBOL = { join: "→", part: "←", quit: "←", kick: "✕", nick: "•", mode: "•", topic: "•" };

const lineOf = new WeakMap(); // rendered .line element -> its line object
const lineCtx = new WeakMap(); // rendered .line element -> { srv, b }

// ---------- replies & reactions (IRCv3 +draft/reply, +draft/react) ----------
function findByMsgid(b, id) {
  if (!id) return null;
  for (let i = b.lines.length - 1; i >= 0; i--) if (b.lines[i].extra?.msgid === id) return b.lines[i];
  return null;
}
// a message you can reply or react to: someone's (or your) chat line with a server msgid
function canAnswer(srv, b, line) {
  return !!line.extra?.msgid && line.type === "msg" && !line.redacted && !line.extra?.dcc && b.type !== "server" &&
    ["privmsg", "action", "notice"].includes(line.kind) && srv?.status === "connected";
}
function applyReact(b, ev) {
  const line = findByMsgid(b, ev.extra?.msgid);
  if (!line || !ev.text) return null;
  const list = (line.reacts ||= []);
  const who = ev.nick || "", lw = who.toLowerCase();
  let r = list.find((x) => x.e === ev.text);
  if (ev.extra?.remove) {
    if (!r) return line;
    r.nicks = r.nicks.filter((n) => n.toLowerCase() !== lw);
    if (!r.nicks.length) list.splice(list.indexOf(r), 1);
  } else {
    if (!r) list.push((r = { e: ev.text, nicks: [] }));
    if (!r.nicks.some((n) => n.toLowerCase() === lw)) r.nicks.push(who);
  }
  return line;
}
function reactsEl(srv, line) {
  const me = (srv.nick || "").toLowerCase();
  const span = document.createElement("span");
  span.className = "reacts";
  span.innerHTML = line.reacts.map((r, i) => {
    const mine = r.nicks.some((n) => n.toLowerCase() === me);
    return `<button class="rx${mine ? " mine" : ""}" data-rx="${i}" title="${esc(r.nicks.join(", "))}"><span class="rx-e">${esc(r.e)}</span><span class="rx-n">${r.nicks.length}</span></button>`;
  }).join("") + (canAnswerSrv(srv) ? `<button class="rx add" data-rx="+" title="${esc(t("Add reaction"))}">${ICONS.smile}</button>` : "");
  return span;
}
function canAnswerSrv(srv) { return srv?.status === "connected"; }
function replyQuote(b, id) {
  const q = findByMsgid(b, id);
  const body = q
    ? `<b style="color:${nickColor(q.nick)}">${esc(q.nick)}</b> <span class="rq-t">${q.redacted ? esc(t("message deleted")) : esc(stripCodes(q.text).slice(0, 160))}</span>`
    : `<i>${esc(t("Reply to an earlier message"))}</i>`;
  return `<span class="rq" data-jump="${esc(id)}"><span class="rq-ar">${ICONS.replyTo}</span>${body}</span>`;
}
// redraws one line in place (reactions changed) without moving the scroll position
function refreshLine(srv, b, line) {
  for (const el of document.querySelectorAll(`.line[data-msgid="${CSS.escape(line.extra.msgid)}"]`)) {
    if (lineOf.get(el) !== line) continue;
    const ctx = lineCtx.get(el);
    el.replaceWith(lineEl(ctx.srv, ctx.b, line));
  }
  if (stick && isActive(srv, b)) scrollBottom();
}
function react(srv, b, line, emoji, remove) {
  Go.React(srv.id, b.name, line.extra.msgid, emoji, !!remove).then((err) => { if (err) toast(err, "error"); });
}
function pickReaction(srv, b, line, anchor) {
  toggleEmojiPicker(anchor, null, (emoji) => {
    const me = (srv.nick || "").toLowerCase();
    const has = line.reacts?.find((r) => r.e === emoji)?.nicks.some((n) => n.toLowerCase() === me);
    if (!has) react(srv, b, line, emoji, false);
  });
}
function startReply(srv, b, line) {
  if (!isActive(srv, b)) select(srv.id, b.key);
  S.reply = { server: srv.id, key: b.key, msgid: line.extra.msgid, nick: line.nick || "", text: line.text || "" };
  renderReplyBar();
  $("input").focus();
}
function cancelReply() {
  if (!S.reply) return false;
  S.reply = null;
  renderReplyBar();
  return true;
}
function renderReplyBar() {
  let bar = $("replyBar");
  if (!bar) {
    bar = document.createElement("div");
    bar.id = "replyBar";
    bar.className = "hidden";
    $("composer").insertBefore(bar, $("composer").querySelector(".input-box"));
    bar.addEventListener("click", (e) => { if (e.target.closest("[data-x]")) { cancelReply(); $("input").focus(); } });
  }
  const r = S.reply;
  const show = !!r && r.server === S.active.server && r.key === S.active.key;
  bar.classList.toggle("hidden", !show);
  if (!show) return;
  bar.innerHTML = `<span class="rb-ico">${ICONS.reply}</span><span class="rb-l">Replying to <b class="no-i18n" style="color:${nickColor(r.nick)}">${esc(r.nick)}</b></span><span class="rb-t no-i18n">${esc(stripCodes(r.text).slice(0, 160))}</span><button class="icon-btn" data-x="1" title="Cancel reply (Esc)">✕</button>`;
}
function jumpToMsg(fromEl, id) {
  const box = fromEl.closest("#chat") || fromEl.closest(".pane-view") || document;
  const target = box.querySelector(`.line[data-msgid="${CSS.escape(id)}"]`);
  if (!target) { toast(t("That message is no longer loaded"), ""); return; }
  target.scrollIntoView({ block: "center", behavior: "smooth" });
  target.classList.remove("flash");
  void target.offsetWidth;
  target.classList.add("flash");
}

// ---------- stacked layout: name and time on their own line, the message underneath ----------
function stackedLayout() {
  const l = S.cfg?.settings?.chatLayout || "";
  return l === "stacked" || (l === "" && isMobile());
}
// a message from the same person shortly after their last one: no new name/time header
function continues(prev, line) {
  return !!prev && prev.type === "msg" && line.type === "msg" && prev.nick === line.nick && !prev.self === !line.self &&
    prev.kind === line.kind && line.kind !== "action" && line.t - prev.t < 5 * 60e3 && line.t >= prev.t &&
    new Date(prev.t).toDateString() === new Date(line.t).toDateString();
}
function prevLine(b, line) {
  const i = b.lines.lastIndexOf(line);
  return i > 0 ? b.lines[i - 1] : null;
}

function lineEl(srv, b, line, prev) {
  if (line.type === "dcc") return dccLineEl(srv, b, line);
  const el = document.createElement("div");
  const isEv = line.type === "event";
  el.className = "line " + [line.kind, isEv ? "ev" : "", line.type === "info" ? "info" : "", line.self ? "self" : "", line.hl ? "hl" : "", line.history ? "history" : "",
    stackedLayout() && continues(prev === undefined ? prevLine(b, line) : prev, line) ? "cont" : ""].join(" ");
  let who = "", msg = "";
  const ctx = ctxFor(srv);
  const nickSpan = (n) => `<span class="nickref" style="color:${nickColor(n)}">${esc(n)}</span>`;
  if (isEv) {
    who = EV_SYMBOL[line.kind] || "•";
    const n = nickSpan(line.nick || "");
    const reason = line.text ? ` <span>(${ircToHtml(line.text, ctx)})</span>` : "";
    switch (line.kind) {
      case "join": msg = `${n} <span>(${esc(line.text)})</span> joined ${esc(b.name)}`; break;
      case "part": msg = `${n} left ${esc(b.name)}${reason}`; break;
      case "quit": msg = `${n} quit${reason}`; break;
      case "kick": msg = `${n} kicked ${nickSpan(line.extra?.victim || "")}${reason}`; break;
      case "nick": msg = `${n} is now known as ${nickSpan(line.text)}`; break;
      case "mode": msg = `${n} sets mode <b>${esc(line.text)}</b>`; break;
      case "topic": msg = `${n} changed the topic to: ${ircToHtml(line.text, ctx)}`; break;
      default: msg = esc(line.text);
    }
  } else if (line.type === "info") {
    who = line.kind === "error" ? "!" : line.kind === "motd" ? "MOTD" : "*";
    msg = ircToHtml(line.text, ctx);
  } else {
    const pfx = line.prefix ? `<span class="pfx">${esc(line.prefix)}</span>` : "";
    who = line.kind === "action" ? "*" : `${pfx}${esc(line.nick)}`;
    msg = line.kind === "action" ? `<span class="nickref" style="color:${nickColor(line.nick)}">${esc(line.nick)}</span> ${ircToHtml(line.text, ctx)}` : ircToHtml(line.text, ctx);
  }
  const whoStyle = !isEv && line.type !== "info" && line.kind !== "action" ? ` style="color:${nickColor(line.nick)}"` : "";
  if (line.redacted) msg = `<span class="redacted">message deleted${line.redactReason ? " · " + esc(line.redactReason) : ""}</span>`;
  const acct = line.extra?.account;
  const title = line.nick ? (acct ? `${line.nick} · logged in as ${acct}` : line.nick) : "";
  const quote = line.type === "msg" && line.extra?.reply ? replyQuote(b, line.extra.reply) : "";
  el.innerHTML = `<span class="ts">${fmtTime(line.t)}</span><span class="who"${whoStyle} data-nick="${esc(line.nick || "")}" title="${esc(title)}">${who}</span><span class="msg">${quote}${msg}</span>`;
  lineOf.set(el, line);
  lineCtx.set(el, { srv, b });
  const mid = line.extra?.msgid;
  if (mid) el.dataset.msgid = mid;
  if (line.type === "msg" && !line.redacted) addMedia(el, line.text);
  if (line.reacts?.length) {
    el.appendChild(document.createElement("span"));
    el.appendChild(document.createElement("span"));
    el.appendChild(reactsEl(srv, line));
  }
  if (canAnswer(srv, b, line)) {
    const act = document.createElement("span");
    act.className = "lact";
    act.innerHTML = `<button data-act="react" title="${esc(t("Add reaction"))}">${ICONS.smile}</button><button data-act="reply" title="${esc(t("Reply"))}">${ICONS.reply}</button>`;
    el.appendChild(act);
  }
  return el;
}

function addMedia(el, text) {
  const set = S.cfg.settings;
  const plain = []; // links that aren't pictures or videos: these get a preview card
  const seen = new Set();
  for (const url of findUrls(text)) {
    const m = mediaFor(url);
    if (!m) { if (/^https?:\/\//i.test(url) && !plain.includes(url)) plain.push(url); continue; }
    if (!set.inlineImages && !set.inlineYouTube && !set.inlineVideos) continue;
    if (seen.has(m.src || m.id)) continue;
    seen.add(m.src || m.id);
    if (seen.size > 3) break;
    const wrap = document.createElement("div");
    wrap.className = "media";
    if (m.type === "image" && set.inlineImages) {
      const img = new Image();
      img.loading = "lazy";
      img.referrerPolicy = "no-referrer";
      img.src = m.src;
      img.dataset.full = m.src;
      img.onerror = () => wrap.remove();
      img.onload = () => { if (stick) scrollBottom(); };
      wrap.appendChild(img);
      const cap = document.createElement("div");
      cap.className = "cap";
      cap.textContent = m.name;
      wrap.appendChild(cap);
    } else if (m.type === "youtube" && set.inlineYouTube) {
      wrap.innerHTML = `<div class="yt" data-yt="${esc(m.id)}" data-start="${m.start}"><img src="https://i.ytimg.com/vi/${esc(m.id)}/hqdefault.jpg" referrerpolicy="no-referrer" alt=""><div class="play"><span></span></div></div>
        <div class="cap"><a href="https://www.youtube.com/watch?v=${esc(m.id)}" data-url="https://www.youtube.com/watch?v=${esc(m.id)}">Open on YouTube</a></div>`;
      wrap.querySelector("img").onload = () => { if (stick) scrollBottom(); };
    } else if (m.type === "video" && set.inlineVideos) {
      const v = document.createElement("video");
      v.src = m.src;
      v.preload = "metadata";
      v.playsInline = true;
      if (m.gif) { v.autoplay = v.loop = v.muted = true; } // gifv: behaves like a gif
      else v.controls = true;
      v.onerror = () => wrap.remove();
      v.onloadedmetadata = () => { if (stick) scrollBottom(); };
      wrap.appendChild(v);
      const cap = document.createElement("div");
      cap.className = "cap";
      cap.textContent = m.name;
      wrap.appendChild(cap);
    } else if (m.type === "audio" && set.inlineVideos) {
      const a = document.createElement("audio");
      a.src = m.src;
      a.preload = "none";
      a.controls = true;
      a.onerror = () => wrap.remove();
      wrap.appendChild(a);
      const cap = document.createElement("div");
      cap.className = "cap";
      cap.textContent = m.name;
      wrap.appendChild(cap);
    } else if (m.type === "embed" && set.inlineVideos) {
      // nothing loads from the site until it's clicked
      const cls = m.provider === "Spotify" ? "yt embed spotify" : "yt embed";
      const title = m.kind ? `${m.provider} · ${m.kind}` : m.provider;
      wrap.innerHTML = `<div class="${cls}" data-embed="${esc(m.src)}"${m.height ? ` data-h="${m.height}"` : ""}><div class="ytitle">${esc(title)}</div><div class="play"><span></span></div></div>
        <div class="cap"><a href="${esc(m.link)}" data-url="${esc(m.link)}">Open on ${esc(m.provider)}</a></div>`;
    } else continue;
    el.appendChild(document.createElement("span"));
    el.appendChild(document.createElement("span"));
    el.appendChild(wrap);
  }
  if (set.linkPreviews !== false && plain.length) addLinkPreviews(el, plain.slice(0, 2));
}

// ---------- DCC (direct file transfers and chats) ----------
S.dcc = {}; // id -> transfer
const dccSpeed = {}; // id -> { done, at, bps }
function dccBufferFor(srv, t) {
  return t.kind === "chat" && t.state === "active" ? null : ensureBuffer(srv, t.nick);
}
function onDCC(tr) {
  const srv = S.byId[tr.server];
  if (!srv) return;
  const prev = S.dcc[tr.id];
  const sp = (dccSpeed[tr.id] ||= { done: 0, at: Date.now(), bps: 0 });
  if (tr.state === "active" && tr.done > sp.done) {
    const dt = (Date.now() - sp.at) / 1000;
    if (dt > 0.2) { sp.bps = sp.bps ? sp.bps * 0.6 + ((tr.done - sp.done) / dt) * 0.4 : (tr.done - sp.done) / dt; sp.done = tr.done; sp.at = Date.now(); }
  }
  S.dcc[tr.id] = tr;
  if (!prev) {
    const b = ensureBuffer(srv, tr.nick);
    addLine(srv, b, { t: tr.time || Date.now(), kind: "dcc", type: "dcc", nick: tr.nick, text: "", dcc: tr.id });
    if (tr.dir === "in" && tr.state === "offer") {
      if (!isActive(srv, b)) { b.hl++; scheduleTree(); }
      toast(tr.kind === "chat" ? t("{0} wants to start a direct chat", tr.nick) : t("{0} wants to send you {1}", tr.nick, tr.file), "");
    }
    return;
  }
  for (const el of document.querySelectorAll(`.line[data-dcc="${CSS.escape(tr.id)}"]`)) {
    const ctx = lineCtx.get(el), line = lineOf.get(el);
    if (ctx && line) el.replaceWith(dccLineEl(ctx.srv, ctx.b, line));
  }
}
function fmtBytes(n) {
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + " GB";
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return Math.round(n / (1 << 10)) + " KB";
  return n + " B";
}
// translated template with HTML pieces dropped into its {0}, {1}…
function tHtml(tpl, ...parts) { return esc(t(tpl)).replace(/\{(\d)\}/g, (_, i) => parts[+i] ?? ""); }
function dccLineEl(srv, b, line) {
  const tr = S.dcc[line.dcc] || { state: "failed", error: "gone", nick: line.nick, kind: "send" };
  const el = document.createElement("div");
  el.className = "line dccl";
  el.dataset.dcc = line.dcc;
  lineOf.set(el, line);
  lineCtx.set(el, { srv, b });
  const nick = `<b class="no-i18n" style="color:${nickColor(tr.nick)}">${esc(tr.nick)}</b>`;
  const file = `<b class="no-i18n">${esc(tr.file || "")}</b>`;
  const size = `<span class="no-i18n">${fmtBytes(tr.size || 0)}</span>`;
  const btn = (act, label, cls = "") => `<button class="btn sm ${cls}" data-dcc-act="${act}">${label}</button>`;
  let title = "", sub = "", acts = "", bar = "";
  const send = tr.kind === "send";
  switch (tr.state) {
    case "offer":
      title = send ? tHtml("{0} wants to send you {1} ({2})", nick, file, size) : tHtml("{0} wants to start a direct chat", nick);
      sub = `<span class="dcc-sub">${tHtml("From {0}. Only accept from people you trust.", esc(tr.addr || ""))}</span>`;
      if (tr.risky) sub += `<span class="dcc-warn">${tHtml("This kind of file can run programs on your computer.")}</span>`;
      acts = btn("accept", "Accept", "primary") + btn("decline", "Decline");
      break;
    case "waiting":
      title = tr.dir === "out" ? (send ? tHtml("Offering {0} ({1}) to {2}…", file, size, nick) : tHtml("Waiting for {0} to accept a direct chat…", nick)) : tHtml("Connecting…");
      if (tr.dir === "out") sub = `<span class="dcc-sub">${tHtml("If they can't connect, you may need to forward a port on your router (Settings → Media → DCC).")}</span>`;
      acts = btn("cancel", "Cancel");
      break;
    case "active": {
      if (!send) { title = tHtml("Direct chat with {0} is open", nick); acts = btn("open", "Open chat", "primary") + btn("cancel", "End chat"); break; }
      const pct = tr.size ? Math.min(100, (tr.done / tr.size) * 100) : 0;
      const sp = dccSpeed[tr.id]?.bps || 0;
      title = tr.dir === "in" ? tHtml("Receiving {0} from {1}", file, nick) : tHtml("Sending {0} to {1}", file, nick);
      sub = `<span class="dcc-sub no-i18n">${fmtBytes(tr.done || 0)} / ${fmtBytes(tr.size || 0)}${sp ? " · " + fmtBytes(Math.round(sp)) + "/s" : ""}</span>`;
      bar = `<span class="dcc-bar"><i style="width:${pct.toFixed(1)}%"></i></span>`;
      acts = btn("cancel", "Cancel");
      break;
    }
    case "done":
      if (!send) title = tHtml("Direct chat with {0} ended", nick);
      else if (tr.dir === "in") { title = tHtml("Received {0} ({1}) from {2}", file, size, nick); acts = btn("show", "Show in folder"); }
      else title = tHtml("Sent {0} ({1}) to {2}", file, size, nick);
      break;
    case "declined": title = send ? tHtml("You declined {0}", file) : tHtml("You declined the direct chat"); break;
    case "cancelled": title = send ? tHtml("Transfer of {0} cancelled", file) : tHtml("Direct chat cancelled"); break;
    default:
      title = send ? tHtml("Transfer of {0} failed", file) : tHtml("Direct chat failed");
      sub = `<span class="dcc-sub">${esc(tr.error || "")}</span>`;
  }
  el.innerHTML = `<span class="ts">${fmtTime(line.t)}</span><span class="who">DCC</span><span class="dcc-card dcc-${tr.state}"><span class="dcc-ico">${send ? "📄" : "💬"}</span><span class="dcc-main no-i18n"><span class="dcc-title">${title}</span>${sub}${bar}</span><span class="dcc-acts">${acts}</span></span>`;
  el.addEventListener("click", (e) => {
    const a = e.target.closest("[data-dcc-act]")?.dataset.dccAct;
    if (!a) return;
    if (a === "accept") Go.DCCAccept(tr.id).then((err) => err && toast(err, "error"));
    else if (a === "decline" || a === "cancel") Go.DCCDecline(tr.id);
    else if (a === "show") Go.ShowDCCFile(tr.id);
    else if (a === "open") select(srv.id, ensureBuffer(srv, "=" + tr.nick).key);
  });
  return el;
}
async function dccPickAndSend(srv, nick) {
  const path = await Go.PickFile(t("Send a file to {0}", nick));
  if (!path) return;
  const err = await Go.DCCSend(srv.id, nick, path);
  if (err) toast(err, "error");
}

// ---------- channel browser (/list) ----------
let chanList = null; // { srv, items, done, el }
function openModal(html, cls = "") {
  const ov = $("overlay");
  ov.innerHTML = `<div class="modal ${cls}">${html}</div>`;
  ov.classList.remove("hidden");
  ov.onclick = (e) => { if (e.target === ov) closeOverlay(); };
  return ov.firstElementChild;
}
function openChannelList(srv, filter = "") {
  const el = openModal(`<div class="modal-head">Channels on <span class="no-i18n" style="margin-left:6px">${esc(srv.network || srv.cfg.name)}</span><button class="icon-btn" data-x="1" style="margin-left:auto">✕</button></div>
    <div class="cl-bar"><input type="text" id="clFilter" placeholder="Filter by name or topic…" value="${esc(filter.replace(/^[*>].*/, ""))}">
      <select id="clMin"><option value="0">Any size</option><option value="5">5+ users</option><option value="20">20+ users</option><option value="100">100+ users</option></select>
      <button class="btn" id="clRefresh">Refresh</button></div>
    <div class="cl-status muted" id="clStatus">Loading the channel list…</div>
    <div class="cl-list" id="clList"></div>`, "lg chanlist");
  chanList = { srv, items: [], done: false, el };
  el.querySelector("[data-x]").onclick = closeOverlay;
  const redraw = () => drawChannelList();
  $("clFilter").addEventListener("input", redraw);
  $("clMin").addEventListener("change", redraw);
  $("clRefresh").onclick = () => { chanList.items = []; chanList.done = false; Go.ListChannels(srv.id, ""); drawChannelList(); };
  $("clList").addEventListener("click", (e) => {
    const row = e.target.closest("[data-ch]");
    if (!row) return;
    if (e.target.closest("[data-join]") || e.detail > 1) {
      Go.SendInput(srv.id, "", "/join " + row.dataset.ch);
      toast(t("Joining {0}…", row.dataset.ch), "ok");
    }
  });
  $("clFilter").focus();
  Go.ListChannels(srv.id, /^[*>]/.test(filter) ? filter : "").then((err) => { if (err) { $("clStatus").textContent = err; } });
}
function drawChannelList() {
  if (!chanList || !document.body.contains(chanList.el)) { chanList = null; return; }
  const q = ($("clFilter").value || "").trim().toLowerCase();
  const min = +$("clMin").value || 0;
  const all = chanList.items;
  const match = all.filter((c) => c.users >= min && (!q || c.name.toLowerCase().includes(q) || c.topic.toLowerCase().includes(q)));
  match.sort((a, b) => b.users - a.users || a.name.localeCompare(b.name));
  const shown = match.slice(0, 300);
  const joined = new Set([...chanList.srv.buffers.values()].filter((b) => b.joined).map((b) => b.key));
  $("clStatus").textContent = chanList.done
    ? t("{0} channels", all.length.toLocaleString()) + (q || min ? " · " + t("{0} matching", match.length.toLocaleString()) : "") + (match.length > shown.length ? " · " + t("showing the {0} biggest", shown.length) : "")
    : t("Loading the channel list… {0} so far", all.length.toLocaleString());
  $("clList").innerHTML = shown.map((c) => `<div class="cl-row" data-ch="${esc(c.name)}" title="${esc(t("Double-click to join"))}">
      <span class="cl-name no-i18n">${esc(c.name)}</span><span class="cl-users">${c.users.toLocaleString()}</span>
      <span class="cl-topic no-i18n">${ircToHtml(c.topic, { mentionRe: null })}</span>
      ${joined.has(c.name.toLowerCase()) ? `<span class="cl-in">Joined</span>` : `<button class="btn sm" data-join="1">Join</button>`}</div>`).join("") ||
    (chanList.done ? `<div class="empty" style="padding:30px">No channels match.</div>` : "");
}
let clDraw = 0;
function onChanList(x) {
  if (!chanList || chanList.srv.id !== x.server) return;
  if (x.start) chanList.items = [];
  chanList.items.push(...(x.items || []));
  if (x.done) chanList.done = true;
  // big networks send tens of thousands: redraw at most a few times a second
  if (!clDraw) clDraw = setTimeout(() => { clDraw = 0; drawChannelList(); }, x.done ? 0 : 300);
}

// ---------- search all history ----------
let searchSeq = 0;
function openSearch(initial = "") {
  const srv = activeServer(), b = activeBuffer();
  const el = openModal(`<div class="modal-head">Search history<button class="icon-btn" data-x="1" style="margin-left:auto">✕</button></div>
    <div class="cl-bar"><input type="text" id="shQ" placeholder="Words to find (nicks count too)" value="${esc(initial)}">
      <select id="shScope"><option value="all">Everywhere</option>${srv ? `<option value="srv">${esc(srv.cfg.name)}</option>` : ""}${b && b.type !== "server" ? `<option value="buf">${esc(b.name)}</option>` : ""}</select></div>
    <div class="cl-status muted" id="shStatus">Type at least 2 letters.</div>
    <div class="sh-wrap"><div class="sh-list" id="shList"></div><div class="sh-ctx hidden" id="shCtx"></div></div>`, "lg search");
  el.querySelector("[data-x]").onclick = closeOverlay;
  let timer = 0;
  const run = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const q = $("shQ")?.value.trim() || "";
      if (q.length < 2) { $("shStatus").textContent = t("Type at least 2 letters."); $("shList").innerHTML = ""; return; }
      const scope = $("shScope").value;
      const seq = ++searchSeq;
      $("shStatus").textContent = t("Searching…");
      const r = await Go.SearchHistory(q, scope === "all" ? "" : srv.id, scope === "buf" ? b.name : "");
      if (seq !== searchSeq || !$("shList")) return;
      const hits = r?.hits || [];
      $("shStatus").textContent = hits.length ? (r.more ? t("Newest {0} matches", hits.length) : t("{0} matches", hits.length)) : t("Nothing found.");
      const words = q.toLowerCase().split(/\s+/).filter(Boolean);
      const mark = (html) => words.reduce((h, w) => h.replace(new RegExp("(" + w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ")(?![^<]*>)", "gi"), "<mark>$1</mark>"), html);
      $("shList").innerHTML = hits.map((h, i) => `<div class="sh-hit" data-i="${i}">
        <div class="sh-where no-i18n"><b>${esc(h.buffer)}</b> · ${esc(h.serverName || "")} · ${esc(new Date(h.time).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }))}</div>
        <div class="sh-text no-i18n"><span style="color:${nickColor(h.nick)};font-weight:600">${esc(h.nick)}</span> ${mark(ircToHtml(h.text, { mentionRe: null }))}</div></div>`).join("");
      $("shList").onclick = (e) => {
        const row = e.target.closest(".sh-hit");
        if (!row) return;
        document.querySelectorAll(".sh-hit.on").forEach((x) => x.classList.remove("on"));
        row.classList.add("on");
        showSearchContext(hits[+row.dataset.i]);
      };
    }, 250);
  };
  $("shQ").addEventListener("input", run);
  $("shScope").addEventListener("change", run);
  $("shQ").focus();
  if (initial) run();
}
async function showSearchContext(h) {
  const box = $("shCtx");
  box.classList.remove("hidden");
  box.innerHTML = `<div class="muted" style="padding:12px">…</div>`;
  const lines = (await Go.HistoryAround(h.server, h.buffer, h.time, 12)) || [];
  const srv = S.byId[h.server];
  box.innerHTML = `<div class="sh-ctx-head"><b class="no-i18n">${esc(h.buffer)}</b>${srv ? `<button class="btn sm primary" id="shGo">Open ${esc(h.buffer)}</button>` : ""}</div>
    <div class="sh-ctx-lines no-i18n">${lines.map((l) => `<div class="sh-l${l.time === h.time && l.text === h.text ? " hit" : ""}"><span class="ts">${fmtTime(l.time)}</span>${
      l.type === "msg" ? `<b style="color:${nickColor(l.nick)}">${esc(l.nick)}</b> ${ircToHtml(l.text || "", { mentionRe: null })}` : `<i>${esc(l.text || l.kind || "")}</i>`}</div>`).join("")}</div>`;
  box.querySelector(".sh-l.hit")?.scrollIntoView({ block: "center" });
  const go = $("shGo");
  if (go) go.onclick = () => {
    closeOverlay();
    const b = ensureBuffer(srv, h.buffer);
    select(srv.id, b.key);
    setTimeout(() => {
      const line = b.lines.find((l) => l.t === h.time && l.text === h.text);
      const el = line && [...$("chat").querySelectorAll(".line")].find((x) => lineOf.get(x) === line);
      if (el) { el.scrollIntoView({ block: "center" }); el.classList.add("flash"); }
      else toast(t("That message is older than what's loaded here; it's shown in the search window."), "");
    }, 150);
  };
}

// ---------- link previews ----------
const previews = new Map(); // url -> Promise<preview>
function getPreview(url) {
  let p = previews.get(url);
  if (!p) {
    p = Go.GetLinkPreview(url).catch(() => null);
    previews.set(url, p);
    if (previews.size > 400) previews.delete(previews.keys().next().value);
  }
  return p;
}
function addLinkPreviews(el, urls) {
  for (const url of urls) {
    const gap1 = document.createElement("span"), gap2 = document.createElement("span");
    const wrap = document.createElement("div");
    wrap.className = "media lp-wrap";
    el.append(gap1, gap2, wrap);
    getPreview(url).then((p) => {
      if (!p || p.error || !p.title) { gap1.remove(); gap2.remove(); wrap.remove(); return; }
      const keep = stick;
      wrap.innerHTML = `<a class="lp${p.image ? " has-img" : ""}" href="${esc(url)}" data-url="${esc(url)}" title="${esc(url)}">
        <span class="lp-txt">${p.site ? `<span class="lp-site">${esc(p.site)}</span>` : ""}<span class="lp-title">${esc(p.title)}</span>${p.description ? `<span class="lp-desc">${esc(p.description)}</span>` : ""}</span>
        ${p.image ? `<img class="lp-img" src="${esc(p.image)}" alt="" loading="lazy" referrerpolicy="no-referrer">` : ""}</a>`;
      const img = wrap.querySelector(".lp-img");
      if (img) {
        img.onerror = () => { img.remove(); wrap.querySelector(".lp")?.classList.remove("has-img"); };
        img.onload = () => { if (keep && stick) scrollBottom(); };
      }
      if (keep && stick) scrollBottom();
    });
  }
}

let stick = true;
let lastRenderedDay = "";
function scrollBottom() { const c = $("chat"); c.scrollTop = c.scrollHeight; }

function renderChat() {
  const chat = $("chat");
  const srv = activeServer(), b = activeBuffer();
  chat.innerHTML = "";
  lastRenderedDay = "";
  chat.classList.toggle("compact", !!S.cfg.settings.compactMode);
  chat.classList.toggle("stacked", stackedLayout());
  if ((!srv || !b) && isSplit() && S.servers.length) {
    chat.innerHTML = `<div class="pane-empty"><div><b>Empty pane</b><br>Pick a channel in the sidebar to show it here,<br>or drag a channel onto this pane.</div></div>`;
    return;
  }
  if (!srv || !b) {
    chat.innerHTML = `<div class="empty"><img src="${logoUrl}" width="96" alt="" style="margin-bottom:12px"><h2>Welcome to IRChatterbox</h2><p>Add a server to get started.</p><p><button class="btn primary" id="emptyAdd">Add server</button></p></div>`;
    $("emptyAdd").onclick = () => editServer();
    return;
  }
  const frag = document.createDocumentFragment();
  let markerEl = null;
  let prev = null;
  b.lines.forEach((line) => {
    const day = new Date(line.t).toDateString();
    if (day !== lastRenderedDay) {
      lastRenderedDay = day;
      const d = document.createElement("div"); d.className = "day"; d.textContent = dayLabel(line.t); frag.appendChild(d);
    }
    if (line === b.marker && S.cfg.settings.markerLine !== false) { frag.appendChild((markerEl = markerLineEl())); prev = null; }
    frag.appendChild(lineEl(srv, b, line, prev));
    prev = line;
  });
  chat.appendChild(frag);
  stick = true;
  scrollBottom();
  // lots unread: start at the marker instead of the bottom
  if (markerEl && markerEl.offsetTop < chat.scrollTop) {
    chat.scrollTop = Math.max(0, markerEl.offsetTop - chat.clientHeight / 3);
    stick = false;
  }
  applyFind();
  newBelow = 0;
  $("toLatest").classList.add("hidden");
  requestAnimationFrame(positionNickHandle);
}

function markerLineEl() {
  const m = document.createElement("div");
  m.className = "marker-line";
  m.innerHTML = `<span>New</span>`;
  m.title = "Messages below arrived while you were away";
  return m;
}

function appendLineDom(b, line) {
  const chat = $("chat");
  const atBottom = chat.scrollHeight - chat.scrollTop - chat.clientHeight < 80;
  const day = new Date(line.t).toDateString();
  if (day !== lastRenderedDay) {
    lastRenderedDay = day;
    const d = document.createElement("div"); d.className = "day"; d.textContent = dayLabel(line.t); chat.appendChild(d);
  }
  const marker = line === b.marker && S.cfg.settings.markerLine !== false;
  const el = lineEl(activeServer(), b, line, marker ? null : undefined);
  if (marker) chat.appendChild(markerLineEl());
  chat.appendChild(el);
  while (chat.childElementCount > MAX_LINES + 50) chat.firstElementChild.remove();
  if (findText) matchLine(el);
  if (chat.childElementCount < 4) requestAnimationFrame(positionNickHandle);
  if (atBottom || line.self) { stick = true; scrollBottom(); }
  else if (!line.history) { newBelow++; updateToLatest(); }
}


// ---------- split view ----------
// Up to four buffers on screen at once. The focused pane is the normal chat (#chat, with the
// header, user list and input following it); the others are live read-only views. Click a
// pane to focus it, pick a channel in the sidebar to show it there, or drag a channel onto it.
const LAYOUTS = { single: 1, cols: 2, rows: 2, grid: 4 };
const panes = []; // runtime: { cell, view } per pane (view is null for the focused one)
// phones get one pane (no room for more); the saved layout comes back on a wide window
function layout() { return LAYOUTS[S.ui.layout] && !isMobile() ? S.ui.layout : "single"; }
function isSplit() { return layout() !== "single"; }
function paneList() {
  const n = LAYOUTS[layout()];
  S.ui.panes = (S.ui.panes || []).slice(0, n);
  while (S.ui.panes.length < n) S.ui.panes.push(null);
  if (!(S.ui.focus >= 0 && S.ui.focus < n)) S.ui.focus = 0;
  return S.ui.panes;
}
function paneBuf(p) {
  const srv = p && S.byId[p.server];
  const b = srv?.buffers.get(p.key);
  return b ? { srv, b } : null;
}
// on screen anywhere: the focused buffer, or one shown in another pane
function onScreen(srv, b) {
  if (isActive(srv, b)) return true;
  if (!isSplit()) return false;
  return paneList().some((p, i) => i !== S.ui.focus && p && p.server === srv.id && p.key === b.key);
}

function buildPanes() {
  const wrap = $("chatWrap"), chat = $("chat"), toLatest = $("toLatest");
  wrap.querySelector(".pane-grid")?.remove();
  panes.length = 0;
  wrap.classList.toggle("split", isSplit());
  if (!isSplit()) { wrap.prepend(chat); wrap.appendChild(toLatest); return; }
  const grid = document.createElement("div");
  grid.className = "pane-grid " + layout();
  paneList().forEach((p, i) => {
    const cell = document.createElement("div");
    cell.className = "pane" + (i === S.ui.focus ? " focus" : "");
    cell.dataset.i = i;
    cell.innerHTML = `<div class="pane-head"><span class="pane-title"></span><button class="pane-x" title="Empty this pane">✕</button></div><div class="pane-body"></div>`;
    const body = cell.querySelector(".pane-body");
    let view = null;
    if (i === S.ui.focus) body.append(chat, toLatest);
    else { view = document.createElement("div"); view.className = "pane-view"; body.appendChild(view); }
    grid.appendChild(cell);
    panes.push({ cell, view });
  });
  wrap.prepend(grid);
}

function paneTitle(srv, b) {
  const name = b.type === "server" ? srv.cfg.name || srv.cfg.host : b.name;
  const style = b.type === "query" ? ` style="color:${nickColor(b.name)}"` : "";
  return `<b${style}>${esc(name)}</b><span class="muted"> · ${esc(srv.network || srv.cfg.name)}</span>`;
}

function renderPanes() {
  if (!isSplit()) return;
  paneList().forEach((p, i) => {
    const pn = panes[i];
    if (!pn) return;
    const pb = paneBuf(p);
    pn.cell.querySelector(".pane-title").innerHTML = pb ? paneTitle(pb.srv, pb.b) : `<span class="muted">Empty pane</span>`;
    pn.cell.classList.toggle("focus", i === S.ui.focus);
    if (pn.view) renderPaneView(pn.view, pb);
    // on screen counts as read
    if (pb && pn.view && S.focused && (pb.b.unread || pb.b.hl)) { pb.b.unread = pb.b.hl = 0; pb.b.activity = false; scheduleTree(); sendRead(pb.srv, pb.b); }
  });
}
let panesTimer = 0;
function renderPanesSoon() { clearTimeout(panesTimer); panesTimer = setTimeout(renderPanes, 80); }

function renderPaneView(view, pb) {
  view.innerHTML = "";
  view.classList.toggle("compact", !!S.cfg.settings.compactMode);
  view.classList.toggle("stacked", stackedLayout());
  view._day = "";
  if (!pb) {
    view.innerHTML = `<div class="pane-empty"><div><b>Empty pane</b><br>Click here, then pick a channel in the sidebar,<br>or drag a channel onto it.</div></div>`;
    return;
  }
  const frag = document.createDocumentFragment();
  let prev = null;
  for (const line of pb.b.lines.slice(-300)) {
    const d = new Date(line.t).toDateString();
    if (d !== view._day) { view._day = d; const el = document.createElement("div"); el.className = "day"; el.textContent = dayLabel(line.t); frag.appendChild(el); }
    frag.appendChild(lineEl(pb.srv, pb.b, line, prev));
    prev = line;
  }
  view.appendChild(frag);
  view.scrollTop = view.scrollHeight;
}

// a new line arrived: show it in any other pane that has this buffer open
function appendToPanes(srv, b, line, outOfOrder) {
  if (!isSplit()) return;
  paneList().forEach((p, i) => {
    if (i === S.ui.focus || !p || p.server !== srv.id || p.key !== b.key) return;
    const view = panes[i]?.view;
    if (!view) return;
    if (outOfOrder || view.querySelector(".pane-empty")) return renderPanesSoon();
    const atBottom = view.scrollHeight - view.scrollTop - view.clientHeight < 80;
    const d = new Date(line.t).toDateString();
    if (d !== view._day) { view._day = d; const el = document.createElement("div"); el.className = "day"; el.textContent = dayLabel(line.t); view.appendChild(el); }
    view.appendChild(lineEl(srv, b, line));
    while (view.childElementCount > 350) view.firstElementChild.remove();
    if (atBottom) view.scrollTop = view.scrollHeight;
  });
}

function focusPane(i) {
  const list = paneList();
  if (i === S.ui.focus || i >= list.length) return;
  S.ui.focus = i;
  saveUi();
  buildPanes();
  const pb = paneBuf(list[i]);
  if (pb) select(pb.srv.id, pb.b.key);
  else { S.active = { server: null, key: null }; renderAll(); }
}

function setLayout(name) {
  const cur = S.active.server ? { server: S.active.server, key: S.active.key } : null;
  S.ui.layout = name;
  if (name !== "single") paneList()[S.ui.focus] = cur;
  saveUi();
  buildPanes();
  if (!isSplit() && !cur) {
    // back to one pane: keep showing something
    const first = paneList()[0] || (S.ui.panes || []).find(Boolean);
    const pb = paneBuf(first);
    if (pb) return select(pb.srv.id, pb.b.key);
  }
  renderAll();
}

// put a buffer in another pane (opening split view if needed)
function showInSplit(srv, b) {
  if (!isSplit()) setLayout("cols");
  const list = paneList();
  let i = list.findIndex((p, j) => j !== S.ui.focus && !p);
  if (i < 0) i = list.findIndex((_, j) => j !== S.ui.focus);
  list[i] = { server: srv.id, key: b.key };
  saveUi();
  renderPanes();
}

function emptyPane(i) {
  const list = paneList();
  list[i] = null;
  saveUi();
  if (i === S.ui.focus) { S.active = { server: null, key: null }; renderAll(); }
  else renderPanes();
}

$("chatWrap").addEventListener("mousedown", (e) => {
  const cell = e.target.closest(".pane");
  if (!cell || e.target.closest(".pane-x")) return;
  const i = +cell.dataset.i;
  if (i !== S.ui.focus) focusPane(i);
});
$("chatWrap").addEventListener("click", (e) => {
  const x = e.target.closest(".pane-x");
  if (x) emptyPane(+x.closest(".pane").dataset.i);
});
$("chatWrap").addEventListener("dragover", (e) => {
  const cell = e.target.closest(".pane");
  if (!cell || drag?.kind !== "channel") return;
  e.preventDefault();
  document.querySelectorAll(".pane.drop-on").forEach((c) => c !== cell && c.classList.remove("drop-on"));
  cell.classList.add("drop-on");
});
$("chatWrap").addEventListener("dragleave", (e) => {
  const cell = e.target.closest(".pane");
  if (cell && !cell.contains(e.relatedTarget)) cell.classList.remove("drop-on");
});
$("chatWrap").addEventListener("drop", (e) => {
  const cell = e.target.closest(".pane");
  if (!cell || drag?.kind !== "channel") return;
  e.preventDefault();
  cell.classList.remove("drop-on");
  const i = +cell.dataset.i, list = paneList();
  // already shown in another pane: swap them
  const j = list.findIndex((p) => p && p.server === drag.srv && p.key === drag.key);
  if (j >= 0 && j !== i) list[j] = list[i];
  list[i] = { server: drag.srv, key: drag.key };
  saveUi();
  if (i === S.ui.focus || j === S.ui.focus) {
    const pb = paneBuf(list[S.ui.focus]);
    if (pb) select(pb.srv.id, pb.b.key); else renderAll();
  } else renderPanes();
});

// window narrowed into phone layout (or widened back): rebuild the panes to match
addEventListener("resize", () => {
  if ($("chatWrap").classList.contains("split") !== isSplit()) { buildPanes(); renderAll(); }
});

$("layoutBtn").onclick = (e) => {
  const r = e.currentTarget.getBoundingClientRect(), cur = layout();
  showMenu(r.left, r.bottom + 4, [
    ["Single", () => setLayout("single"), "", cur === "single"],
    ["Side by side", () => setLayout("cols"), "", cur === "cols"],
    ["Top and bottom", () => setLayout("rows"), "", cur === "rows"],
    ["Four (2 × 2)", () => setLayout("grid"), "", cur === "grid"],
  ], "Split view");
};

// Round "jump to latest" arrow: shown whenever you've scrolled up, with a count of new lines below.
let newBelow = 0;
function updateToLatest() {
  const c = $("chat");
  const away = c.scrollHeight - c.scrollTop - c.clientHeight > 150;
  if (!away) newBelow = 0;
  $("toLatest").classList.toggle("hidden", !away);
  $("toLatestN").textContent = newBelow ? (newBelow > 99 ? "99+" : newBelow) : "";
}
$("chat").addEventListener("scroll", () => {
  const c = $("chat");
  stick = c.scrollHeight - c.scrollTop - c.clientHeight < 80;
  updateToLatest();
});
$("toLatest").onclick = () => { $("chat").scrollTo({ top: $("chat").scrollHeight, behavior: "smooth" }); newBelow = 0; };

// chat interactions
$("chat").addEventListener("click", (e) => {
  const a = e.target.closest("a[data-url]");
  if (a) { e.preventDefault(); Go.OpenURL(a.dataset.url); return; }
  const yt = e.target.closest(".yt[data-yt]");
  if (yt && !yt.querySelector("iframe")) {
    const id = yt.dataset.yt, start = +yt.dataset.start || 0;
    yt.innerHTML = `<iframe src="https://www.youtube-nocookie.com/embed/${id}?autoplay=1&rel=0${start ? "&start=" + start : ""}" allow="autoplay; encrypted-media; picture-in-picture; fullscreen" allowfullscreen referrerpolicy="strict-origin-when-cross-origin"></iframe>`;
    return;
  }
  const em = e.target.closest(".yt[data-embed]");
  if (em && !em.querySelector("iframe")) {
    const f = document.createElement("iframe");
    f.src = em.dataset.embed; // built by mediaFor from a fixed host + an id, never the raw link
    f.allow = "autoplay; encrypted-media; picture-in-picture; fullscreen";
    f.allowFullscreen = true;
    f.referrerPolicy = "strict-origin-when-cross-origin";
    if (em.dataset.h) em.style.height = em.dataset.h + "px";
    em.replaceChildren(f);
    return;
  }
  const img = e.target.closest(".media img[data-full]");
  if (img) { lightbox(img.dataset.full); return; }
  const who = e.target.closest(".who[data-nick]");
  if (who && who.dataset.nick) insertMention(who.dataset.nick);
});
$("chat").addEventListener("dblclick", (e) => {
  const who = e.target.closest(".who[data-nick], .nickref");
  const nick = who?.dataset.nick || who?.textContent;
  if (nick) openQuery(nick);
});
$("chat").addEventListener("contextmenu", (e) => {
  const who = e.target.closest(".who[data-nick], .nickref");
  const nick = who?.dataset?.nick || who?.textContent;
  if (nick) { e.preventDefault(); userMenu(nick, e); return; }
  const a = e.target.closest("a[data-url]");
  if (a) {
    e.preventDefault();
    showMenu(e.clientX, e.clientY, [["Open link", () => Go.OpenURL(a.dataset.url)], ["Copy link", () => navigator.clipboard.writeText(a.dataset.url)]]);
    return;
  }
  // your own message with a server msgid: offer to delete it (IRCv3 message-redaction)
  // a chat line: copy, delete your own (IRCv3 message-redaction), and script items
  const lineElm = e.target.closest(".line");
  const line = lineElm && lineOf.get(lineElm);
  if (!line || line.redacted || window.getSelection()?.toString() || !["privmsg", "action", "notice"].includes(line.kind)) return;
  e.preventDefault();
  const srv = activeServer(), b = activeBuffer();
  if (!srv || !b) return;
  const items = [];
  if (canAnswer(srv, b, line)) {
    items.push(["Reply", () => startReply(srv, b, line)]);
    items.push(["Add reaction…", () => pickReaction(srv, b, line, lineElm.querySelector(".lact") || lineElm)]);
  }
  items.push(["Copy text", () => navigator.clipboard.writeText(line.text)]);
  if (line.self && line.extra?.msgid) items.push(["Delete message", async () => {
    const err = await Go.Redact(srv.id, b.name, line.extra.msgid);
    if (err) toast(err, "error");
  }, "danger"]);
  if (!line.self && line.nick) items.push(["Report…", () => reportUser(srv, b, line.nick, line)]);
  const x = e.clientX, y = e.clientY;
  withPluginItems(items, { server: srv.id, buffer: b.name, kind: "message", nick: line.nick || "", text: line.text || "" })
    .then((all) => showMenu(x, y, all));
});
document.addEventListener("click", (e) => {
  const lineElm = e.target.closest(".line");
  const hit = lineElm && e.target.closest(".rq[data-jump], .lact [data-act], .rx[data-rx]");
  if (hit) {
    e.preventDefault();
    const line = lineOf.get(lineElm), ctx = lineCtx.get(lineElm);
    if (!line || !ctx) return;
    if (hit.matches(".rq")) return jumpToMsg(lineElm, hit.dataset.jump);
    if (hit.dataset.act === "reply") return startReply(ctx.srv, ctx.b, line);
    if (hit.dataset.act === "react" || hit.dataset.rx === "+") return pickReaction(ctx.srv, ctx.b, line, hit);
    const r = line.reacts?.[+hit.dataset.rx];
    if (r) react(ctx.srv, ctx.b, line, r.e, hit.classList.contains("mine"));
    return;
  }
  const a = e.target.closest("a[href]");
  if (a && !e.defaultPrevented) { e.preventDefault(); Go.OpenURL(a.dataset.url || a.href); }
});

function insertMention(nick) {
  const inp = $("input");
  if (!inp.value) inp.value = nick + ": ";
  else inp.value += (inp.value.endsWith(" ") ? "" : " ") + nick + " ";
  inp.focus();
  autosize();
}

function lightbox(src) {
  const ov = $("overlay");
  ov.innerHTML = `<img class="lightbox" src="${esc(src)}" referrerpolicy="no-referrer">`;
  ov.classList.remove("hidden");
  ov.onclick = () => closeOverlay();
}
export function closeOverlay() {
  const ov = $("overlay");
  ov.classList.add("hidden");
  ov.innerHTML = "";
  ov.onclick = null;
}

function openQuery(nick) {
  const srv = activeServer();
  if (!srv) return;
  Go.SendInput(srv.id, "", "/query " + nick);
}

// ---------- user list ----------
function renderUsers() {
  const b = activeBuffer();
  const body = $("ulBody");
  if (!b || b.type !== "channel") { body.innerHTML = ""; return; }
  const f = $("ulFilter").value.trim().toLowerCase();
  const groups = {};
  for (const u of b.users) {
    if (f && !u.nick.toLowerCase().includes(f)) continue;
    const top = [...(u.prefix || "")].sort((x, y) => RANKS.indexOf(x) - RANKS.indexOf(y))[0] || "";
    (groups[top] ||= []).push(u);
  }
  let html = "";
  for (const r of [...RANKS, ""]) {
    const list = groups[r];
    if (!list?.length) continue;
    list.sort((a, b) => a.nick.toLowerCase().localeCompare(b.nick.toLowerCase()));
    html += `<div class="ul-group">${GROUPS[r]} — ${list.length}</div>`;
    const shown = list.slice(0, 1500);
    for (const u of shown)
      html += `<div class="ul-user${u.away ? " away" : ""}" data-nick="${esc(u.nick)}" title="${esc(u.nick)}${u.away ? " (away)" : ""}"><span class="p">${esc(r)}</span><span class="n" style="color:${nickColor(u.nick)}">${esc(u.nick)}</span></div>`;
  }
  body.innerHTML = html;
  $("ulTitle").textContent = `Users — ${b.users.length}`;
}
$("ulFilter").addEventListener("input", renderUsers);
$("ulBody").addEventListener("dblclick", (e) => { const u = e.target.closest(".ul-user"); if (u) openQuery(u.dataset.nick); });
$("ulBody").addEventListener("click", (e) => { const u = e.target.closest(".ul-user"); if (u) insertMention(u.dataset.nick); });
$("ulBody").addEventListener("contextmenu", (e) => {
  const u = e.target.closest(".ul-user");
  if (u) { e.preventDefault(); userMenu(u.dataset.nick, e); }
});

// ---------- report ----------
// Report someone (from their nick) or one message: blocks them if asked and sends the report,
// with the lines around it, for review.
function reportUser(srv, b, nick, line) {
  const lines = (b?.lines || []).filter((l) => ["privmsg", "action", "notice"].includes(l.kind));
  let end = line ? lines.indexOf(line) : -1;
  if (end < 0) { // from a nick: their latest message
    for (let i = lines.length - 1; i >= 0; i--) if ((lines[i].nick || "").toLowerCase() === nick.toLowerCase()) { end = i; break; }
  }
  const target = end >= 0 ? lines[end] : null;
  const ctx = (end >= 0 ? lines.slice(Math.max(0, end - 9), end + 1) : []).map((l) => {
    const d = new Date(l.t || Date.now());
    const ts = d.toISOString().replace("T", " ").slice(0, 19);
    return l.kind === "action" ? `[${ts}] * ${l.nick} ${stripCodes(l.text || "")}` : `[${ts}] <${l.nick}> ${stripCodes(l.text || "")}`;
  });
  openReportDialog({ nick, where: b?.name || srv.cfg.name, message: target ? stripCodes(target.text || "") : "" }, async ({ reason, note, block }) => {
    const err = await Go.SendReport({ server: srv.id, buffer: b?.name || "", nick, reason, note, message: target ? stripCodes(target.text || "") : "", context: ctx });
    if (err) throw new Error(err);
    if (block) Go.SendInput(srv.id, b?.name || "", "/ignore " + nick);
    toast(block ? `Reported and blocked ${nick}` : `Reported ${nick}`, "ok");
  });
}

// Kick / ban from the user list: straight away, or after a confirmation with a reason box
// (Settings → General → Moderation).
function moderate(kind, srv, b, nick, send) {
  const set = S.cfg.settings;
  const reasonOf = (r) => (r || "").trim() || (set.kickReason || "").trim() || (kind === "kb" ? "Banned" : nick);
  const run = (r) => {
    if (kind === "kick") send(`/kick ${nick} ${reasonOf(r)}`);
    else if (kind === "ban") send(`/ban ${nick}`);
    else send(`/kb ${nick} ${reasonOf(r)}`);
  };
  if (set.confirmModeration === false) return run("");
  const title = { kick: "Kick {0} from {1}?", ban: "Ban {0} from {1}?", kb: "Kick and ban {0} from {1}?" }[kind];
  const what = {
    kick: "They'll be removed from the channel, and can rejoin.",
    ban: "They won't be able to join again until the ban is removed. Anyone else from the same address is banned too. They aren't removed if they're already in the channel.",
    kb: "They'll be removed from the channel and won't be able to join again until the ban is removed.",
  }[kind];
  const ok = { kick: "Kick", ban: "Ban", kb: "Kick + ban" }[kind];
  const el = openModal(`<div class="modal-head no-i18n"><span>${tHtml(title, `<b style="color:${nickColor(nick)}">${esc(nick)}</b>`, esc(b.name))}</span></div>
    <div class="modal-body"><p class="muted" style="margin:0 0 12px">${esc(t(what))}</p>
    ${kind === "ban" ? "" : `<div class="field"><label>Reason</label><input type="text" id="modReason" maxlength="300" placeholder="${esc(set.kickReason || (kind === "kb" ? "Banned" : nick))}"></div><div class="hint">Everyone in the channel sees the reason.</div>`}</div>
    <div class="modal-foot"><button class="btn" id="modCancel">Cancel</button><button class="btn danger-fill" id="modOk">${esc(t(ok))}</button></div>`, "sm");
  const go = () => { const r = $("modReason")?.value || ""; closeOverlay(); run(r); };
  $("modCancel").onclick = closeOverlay;
  $("modOk").onclick = go;
  $("modReason")?.addEventListener("keydown", (e) => { if (e.key === "Enter") go(); });
  ($("modReason") || $("modOk")).focus();
  return el;
}

function userMenu(nick, e) {
  const srv = activeServer(), b = activeBuffer();
  if (!srv) return;
  const send = (cmd) => Go.SendInput(srv.id, b?.name || "", cmd);
  const items = [
    ["Message", () => openQuery(nick)],
    ["Whois", () => send("/whois " + nick)],
    ["Mention", () => insertMention(nick)],
  ];
  if (!android && srv.status === "connected" && nick.toLowerCase() !== (srv.nick || "").toLowerCase()) {
    items.push(["Send file…", () => dccPickAndSend(srv, nick)]);
    items.push(["Direct chat (DCC)", () => Go.DCCChat(srv.id, nick).then((err) => err && toast(err, "error"))]);
  }
  if (b?.type === "channel") {
    const u = b.users.find((x) => x.nick === nick);
    const p = u?.prefix || "";
    items.push("-");
    items.push(p.includes("@") ? ["Deop", () => send("/deop " + nick)] : ["Op", () => send("/op " + nick)]);
    items.push(p.includes("+") ? ["Devoice", () => send("/devoice " + nick)] : ["Voice", () => send("/voice " + nick)]);
    const ask = S.cfg.settings.confirmModeration !== false;
    items.push([ask ? "Kick…" : "Kick", () => moderate("kick", srv, b, nick, send)]);
    items.push([ask ? "Ban…" : "Ban", () => moderate("ban", srv, b, nick, send), "danger"]);
    items.push([ask ? "Kick + ban…" : "Kick + ban", () => moderate("kb", srv, b, nick, send), "danger"]);
  }
  items.push("-");
  const custom = S.cfg.nickColors?.[nick.toLowerCase()] || S.cfg.nickColors?.[baseNick(nick)] || "";
  items.push(["Set nick colour…", () => openColorPicker(nick, custom, (c) => Go.SetNickColor(nick, c))]);
  if (custom) items.push(["Reset nick colour", () => Go.SetNickColor(nick, "")]);
  items.push(["Report…", () => reportUser(srv, b, nick, null)]);
  items.push(["Block / Ignore", () => send("/ignore " + nick), "danger"]);
  const x = e.clientX, y = e.clientY;
  withPluginItems(items.filter((i) => i !== "danger"), { server: srv.id, buffer: b?.name || "", kind: "nicklist", nick })
    .then((all) => showMenu(x, y, all, nick));
}

// double-click the topic to edit it
$("hTopic").addEventListener("dblclick", (e) => {
  const srv = activeServer(), b = activeBuffer();
  if (!srv || b?.type !== "channel") return;
  if (e.target.closest("a")) return;
  window.getSelection()?.removeAllRanges();
  openPrompt(`Topic for ${b.name}`, "New topic (leave empty to clear)", b.topic || "", (v) => {
    if (v === undefined || v === (b.topic || "")) return;
    Go.SendInput(srv.id, b.name, v ? `/topic ${b.name} ${v}` : `/raw TOPIC ${b.name} :`);
  }, "Set topic");
});

// ---------- update banner ----------
export function showUpdateBar(info) {
  const bar = $("updateBar");
  bar.innerHTML = `<span class="ub-text"><b>IRChatterbox ${esc(info.latest)}</b> is available — you have ${esc(info.current)}.</span>
    <span class="ub-progress hidden"><i></i></span>
    <button class="btn sm primary" id="ubInstall">${android || info.manual ? "Download update" : "Update &amp; restart"}</button>
    <button class="btn sm" id="ubNotes">What's new</button>
    <button class="btn sm" id="ubSkip">Skip this version</button>
    <button class="icon-btn" id="ubClose" title="Later">✕</button>`;
  bar.classList.remove("hidden");
  $("ubNotes").onclick = () => Go.OpenURL(info.url);
  $("ubSkip").onclick = () => { Go.SkipUpdateVersion(info.latest); bar.classList.add("hidden"); };
  $("ubClose").onclick = () => bar.classList.add("hidden");
  $("ubInstall").onclick = async () => {
    // ✕ always works, so the bar can be put away while (or after) the download runs
    bar.querySelectorAll("button:not(#ubClose)").forEach((b) => (b.disabled = true));
    bar.querySelector(".ub-progress").classList.remove("hidden");
    bar.querySelector(".ub-text").innerHTML = android || info.manual ? `Opening the download…` : `Downloading IRChatterbox ${esc(info.latest)}…`;
    const err = await Go.InstallUpdate();
    if (err) {
      toast(err, "error");
      bar.querySelector(".ub-text").textContent = err;
      bar.querySelectorAll("button").forEach((b) => (b.disabled = false));
      bar.querySelector(".ub-progress").classList.add("hidden");
    } else {
      bar.querySelector(".ub-text").textContent = android ? "Open the downloaded file to install it."
        : info.manual ? "Unzip the download and move IRChatterbox into Applications, replacing the old one." : "Installed — restarting…";
      if (android || info.manual) {
        // the download happens in the browser: nothing more to wait for here
        bar.querySelector(".ub-progress").classList.add("hidden");
        bar.querySelectorAll("button").forEach((b) => (b.disabled = false));
      }
    }
  };
}

// ---------- typing (IRCv3 +typing) ----------
// Others: "Bob is typing…" above the input. Active expires after 6s, paused after 30s.
function setTyping(srv, b, nick, state) {
  b.typing ||= new Map();
  clearTimeout(b.typing.get(nick)?.timer);
  if (state === "done") b.typing.delete(nick);
  else {
    const timer = setTimeout(() => { b.typing.delete(nick); if (isActive(srv, b)) renderTyping(); }, state === "paused" ? 30000 : 6000);
    b.typing.set(nick, { state, timer });
  }
  if (isActive(srv, b)) renderTyping();
}
function renderTyping() {
  const b = activeBuffer(), el = $("typingNow");
  const who = b?.typing ? [...b.typing.entries()].filter(([, v]) => v.state === "active").map(([n]) => n) : [];
  el.textContent = !who.length ? "" : who.length === 1 ? `${who[0]} is typing…` : who.length === 2 ? `${who[0]} and ${who[1]} are typing…` : "Several people are typing…";
  el.classList.toggle("on", who.length > 0);
}
// Ours: "active" at most every 3s while typing, "paused" after 5s idle, "done" on send/clear.
let typingSent = { key: "", at: 0, state: "" }, typingIdle = 0;
function typingTarget() {
  const srv = activeServer(), b = activeBuffer();
  return srv && b && b.type !== "server" ? { srv, b } : null;
}
function sendTypingState(state) {
  const t = typingTarget();
  if (!t || S.cfg.settings.sendTyping === false) return;
  const key = t.srv.id + "\0" + t.b.key;
  if (state === "active" && typingSent.key === key && typingSent.state === "active" && Date.now() - typingSent.at < 3000) return;
  if (state !== "active" && (typingSent.key !== key || typingSent.state === "done" || !typingSent.state)) return;
  typingSent = { key, at: Date.now(), state };
  Go.SendTyping(t.srv.id, t.b.name, state);
}
function onTypingInput() {
  clearTimeout(typingIdle);
  const v = input.value;
  if (!v.trim() || v.startsWith("/")) { sendTypingState("done"); return; }
  sendTypingState("active");
  typingIdle = setTimeout(() => sendTypingState("paused"), 5000);
}

// ---------- "closed unexpectedly" notice ----------
async function showCrashNotice() {
  if (android) return;
  const d = await Go.GetDiagnostics();
  if (!d?.lastCrash) return;
  const bar = $("noticeBar");
  bar.innerHTML = `<span class="ub-text"><b>IRChatterbox closed unexpectedly last time.</b> A crash report was saved — sending it helps get it fixed.</span>
    <button class="btn sm primary" id="nbReport">Report problem</button>
    <button class="btn sm" id="nbLogs">Open logs</button>
    <button class="icon-btn" id="nbClose" title="Dismiss">✕</button>`;
  bar.classList.remove("hidden");
  const close = () => { bar.classList.add("hidden"); Go.DismissCrash(); };
  $("nbReport").onclick = () => { Go.ReportProblem(); close(); };
  $("nbLogs").onclick = () => Go.OpenDiagnostics();
  $("nbClose").onclick = close;
}

// ---------- toast ----------
let toastTimer;
export function toast(msg, kind = "ok") {
  const t = $("toast");
  t.textContent = msg;
  t.className = kind;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add("hidden"), 2600);
}

// ---------- context menu ----------
// items: "-" or [label, fn | subItems[], className, checked]. A submenu opens in place,
// with a Back item, so it works the same with a mouse and on a phone.
export function showMenu(x, y, items, header, parent) {
  const m = $("menu");
  const back = parent ? `<button class="mi back" data-back="1">‹ ${esc(parent.header || "Back")}</button><div class="sep"></div>` : "";
  m.innerHTML = (header ? `<div class="mh">${esc(header)}</div>` : "") + back + items.map((it, i) =>
    it === "-" ? `<div class="sep"></div>` : `<button class="mi ${it[2] || ""}${it.length > 3 ? " chk" : ""}${it[3] ? " on" : ""}${Array.isArray(it[1]) ? " sub" : ""}" data-i="${i}">${esc(it[0])}</button>`).join("");
  m.classList.remove("hidden");
  const r = m.getBoundingClientRect();
  m.style.left = Math.max(8, Math.min(x, innerWidth - r.width - 8)) + "px";
  m.style.top = Math.max(8, Math.min(y, innerHeight - r.height - 8)) + "px";
  m.onclick = (e) => {
    if (e.target.closest("[data-back]")) return showMenu(x, y, parent.items, parent.header, parent.parent);
    const b = e.target.closest("[data-i]");
    if (!b) return;
    const it = items[+b.dataset.i];
    if (Array.isArray(it[1])) return showMenu(x, y, it[1], it[0], { items, header, parent });
    hideMenu();
    it[1]();
  };
}

// Script-added menu items (mIRC "menu" blocks and the JS menu() call), appended after a
// separator. kind: nicklist, channel, query, status or message.
async function withPluginItems(items, ctx) {
  let extra = null;
  try { extra = await Go.PluginMenu(ctx); } catch { /* scripts off or not supported */ }
  if (!extra?.length) return items;
  const conv = (list) => list.map((it) => it.sep ? "-" : it.items ? [it.label, conv(it.items)] : [it.label, () => Go.RunPluginMenu(ctx, it.id)]);
  return items.length ? [...items, "-", ...conv(extra)] : conv(extra);
}
function hideMenu() { $("menu").classList.add("hidden"); }
document.addEventListener("mousedown", (e) => { if (!e.target.closest("#menu")) hideMenu(); });
window.addEventListener("blur", hideMenu);

// ---------- composer ----------
const input = $("input");
function autosize() {
  input.style.height = "auto";
  input.style.height = Math.min(input.scrollHeight, 160) + "px";
}
input.addEventListener("input", () => { autosize(); updateCompletions(); tabState = null; onTypingInput(); const srv = activeServer(), b = activeBuffer(); if (srv && b) { inputFor = draftKey(srv, b); saveDraft(srv, b); } });

const COMMANDS = [
  ["join", "#channel [key]"], ["part", "[#channel] [message]"], ["msg", "nick message"], ["query", "nick [message]"], ["me", "action"],
  ["nick", "newnick"], ["topic", "[new topic]"], ["kick", "nick [reason]"], ["ban", "nick|mask"], ["unban", "mask"], ["kb", "nick [reason]"],
  ["op", "nick…"], ["deop", "nick…"], ["voice", "nick…"], ["devoice", "nick…"], ["halfop", "nick…"], ["mode", "[target] modes"],
  ["whois", "nick"], ["invite", "nick [#channel]"], ["away", "[message]"], ["back", ""], ["notice", "target text"], ["ctcp", "nick COMMAND"],
  ["ping", "nick"], ["names", "[#channel]"], ["list", "[pattern]"], ["cycle", ""], ["close", ""], ["clear", ""], ["ignore", "nick|mask"],
  ["unignore", "nick|mask"], ["color", "nick #hex|reset"], ["highlight", "add|del|list word"], ["theme", "name|auto"], ["raw", "IRC LINE"],
  ["quote", "IRC LINE"], ["connect", ""], ["reconnect", ""], ["disconnect", "[message]"], ["quit", "[message]"], ["server", "name"],
  ["ns", "NickServ command"], ["cs", "ChanServ command"], ["umode", "modes"], ["reload", "reload plugins"], ["np", "share the song you're playing"], ["settings", ""], ["shortcut", "add|del|list $name text"], ["help", ""],
];

// Completion popup: "/" commands at the start of the line, "$" text shortcuts anywhere.
let compSel = 0, compItems = [], compTyped = "";
let pluginCommands = [];
function shortcutWordBeforeCaret() {
  const before = input.value.slice(0, input.selectionStart);
  const m = /(^|\s)(\$[^\s$]*)$/.exec(before);
  return m ? { word: m[2], start: before.length - m[2].length } : null;
}
function findShortcut(word) {
  return (S.cfg.shortcuts || []).find((sc) => sc.trigger.toLowerCase() === word.toLowerCase());
}
function replaceRange(start, end, text) {
  input.setRangeText(text, start, end, "end");
  autosize();
}
function updateCompletions() {
  const v = input.value;
  const box = document.getElementById("completeBox");
  compItems = [];
  if (v.startsWith("/") && !v.includes(" ") && !v.includes("\n")) {
    const q = v.slice(1).toLowerCase();
    compTyped = v;
    const all = [...COMMANDS, ...pluginCommands.filter((p) => !COMMANDS.some(([c]) => c === p)).map((p) => [p, "plugin command"])];
    compItems = all.filter(([c]) => c.startsWith(q)).slice(0, 12).map(([c, d]) => ({
      label: "/" + c, desc: d, apply: () => { input.value = "/" + c + " "; },
    }));
  } else {
    const sw = shortcutWordBeforeCaret();
    if (sw) {
      const q = sw.word.toLowerCase();
      compTyped = sw.word;
      compItems = (S.cfg.shortcuts || []).filter((sc) => sc.trigger.toLowerCase().startsWith(q)).slice(0, 12).map((sc) => ({
        label: sc.trigger, desc: sc.text, apply: () => replaceRange(sw.start, sw.start + sw.word.length, sc.text + " "),
      }));
    }
  }
  if (!compItems.length) { box?.remove(); return; }
  compSel = Math.min(compSel, compItems.length - 1);
  const el = box || Object.assign(document.createElement("div"), { id: "completeBox" });
  el.innerHTML = compItems.map((it, i) => `<div class="c ${i === compSel ? "sel" : ""}" data-i="${i}"><b>${esc(it.label)}</b><span class="d">${esc(it.desc)}</span></div>`).join("");
  document.body.appendChild(el);
  const r = input.closest(".input-box").getBoundingClientRect();
  el.style.left = r.left + "px";
  el.style.bottom = innerHeight - r.top + 6 + "px";
  el.onmousedown = (e) => { e.preventDefault(); const c = e.target.closest("[data-i]"); if (c) acceptCompletion(+c.dataset.i); };
}
function acceptCompletion(i) {
  compItems[i]?.apply();
  document.getElementById("completeBox")?.remove();
  compItems = [];
  input.focus();
}

let tabState = null;
function tabComplete() {
  const b = activeBuffer(), srv = activeServer();
  if (!b) return;
  const pos = input.selectionStart;
  const before = input.value.slice(0, pos);
  if (!tabState) {
    const m = /(\S*)$/.exec(before);
    const word = m[1].toLowerCase();
    if (!word) return;
    let names = b.type === "channel" ? b.users.map((u) => u.nick) : [b.name];
    if (word.startsWith("#")) names = [...srv.buffers.values()].filter((x) => x.type === "channel").map((x) => x.name);
    // recently active speakers first
    const recent = [];
    for (let i = b.lines.length - 1; i >= 0 && recent.length < 50; i--) { const n = b.lines[i].nick; if (n && !recent.includes(n)) recent.push(n); }
    const matches = names.filter((n) => n.toLowerCase().startsWith(word))
      .sort((x, y) => { const rx = recent.indexOf(x), ry = recent.indexOf(y); return (rx < 0 ? 999 : rx) - (ry < 0 ? 999 : ry) || x.localeCompare(y); });
    if (!matches.length) return;
    tabState = { start: pos - m[1].length, matches, i: 0, atStart: pos - m[1].length === 0, after: input.value.slice(pos) };
  } else {
    tabState.i = (tabState.i + 1) % tabState.matches.length;
  }
  const n = tabState.matches[tabState.i];
  const suffix = tabState.atStart && !n.startsWith("#") ? ": " : " ";
  input.value = input.value.slice(0, tabState.start) + n + suffix + tabState.after;
  const caret = tabState.start + n.length + suffix.length;
  input.setSelectionRange(caret, caret);
}

input.addEventListener("keydown", (e) => {
  const box = document.getElementById("completeBox");
  if (box && compItems.length) {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); compSel = (compSel + (e.key === "ArrowDown" ? 1 : -1) + compItems.length) % compItems.length; updateCompletions(); return; }
    const exact = compItems.some((it) => it.label.toLowerCase() === compTyped.toLowerCase());
    if (e.key === "Tab" || (e.key === "Enter" && compTyped.length > 1 && !exact)) { e.preventDefault(); acceptCompletion(compSel); return; }
    if (e.key === "Escape") { box.remove(); compItems = []; return; }
  }
  // expand a shortcut as soon as you type the space after it
  if (e.key === " " && input.selectionStart === input.selectionEnd && !input.value.toLowerCase().startsWith("/shortcut")) {
    const sw = shortcutWordBeforeCaret();
    const sc = sw && findShortcut(sw.word);
    if (sc) replaceRange(sw.start, sw.start + sw.word.length, sc.text);
  }
  if (e.key === "Tab") { e.preventDefault(); tabComplete(); return; }
  if (e.key === "Escape" && cancelReply()) { e.preventDefault(); return; }
  if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); return; }
  if (e.key === "ArrowUp" && !input.value.slice(0, input.selectionStart).includes("\n") && S.inputHistory.length) {
    e.preventDefault();
    S.histPos = Math.min(S.histPos + 1, S.inputHistory.length - 1);
    input.value = S.inputHistory[S.histPos]; autosize();
    return;
  }
  if (e.key === "ArrowDown" && S.histPos >= 0 && !input.value.slice(input.selectionEnd).includes("\n")) {
    e.preventDefault();
    S.histPos--;
    input.value = S.histPos >= 0 ? S.inputHistory[S.histPos] : ""; autosize();
    return;
  }
  if (e.ctrlKey && ["b", "u", "i"].includes(e.key.toLowerCase())) {
    e.preventDefault();
    const code = { b: "\x02", u: "\x1f", i: "\x1d" }[e.key.toLowerCase()];
    const s = input.selectionStart, en = input.selectionEnd;
    input.setRangeText(code + input.value.slice(s, en) + code, s, en, "end");
  }
});
$("sendBtn").onclick = send;
function openEmoji() {
  toggleEmojiPicker($("emojiBtn"), input, (em) => {
    const s = input.selectionStart ?? input.value.length, e = input.selectionEnd ?? s;
    input.setRangeText(em, s, e, "end");
    autosize();
    input.focus();
  });
}
$("emojiBtn").onclick = openEmoji;

// ---------- image uploads: attach button, paste, drag and drop ----------
const UPLOAD_TYPES = ["image/jpeg", "image/png", "image/webp", "image/bmp"];
const UPLOAD_MAX = 10 * 1024 * 1024;
let uploading = 0;

// Phone photos are large (4000×3000, several MB) and carry EXIF metadata, GPS location
// included. Before uploading, anything big is redrawn at most 2048 px on its longest side
// and saved as JPEG: a fraction of the size, the right way up, and without the metadata.
const SHRINK_EDGE = 2048;
async function shrinkForUpload(file) {
  const big = file.size > 1.5 * 1024 * 1024;
  const convert = !UPLOAD_TYPES.includes(file.type); // e.g. HEIC from a phone camera, if the browser can read it
  if (!(big || convert || file.type === "image/jpeg" || file.type === "image/bmp") || typeof createImageBitmap !== "function") return file;
  let bmp;
  try { bmp = await createImageBitmap(file, { imageOrientation: "from-image" }); } catch { return file; }
  const scale = Math.min(1, SHRINK_EDGE / Math.max(bmp.width, bmp.height));
  // a small PNG/WebP (a screenshot) stays as it is: re-encoding text as JPEG blurs it
  if (scale === 1 && !big && !convert && file.type !== "image/jpeg" && file.type !== "image/bmp") { bmp.close?.(); return file; }
  const c = document.createElement("canvas");
  c.width = Math.round(bmp.width * scale); c.height = Math.round(bmp.height * scale);
  const g = c.getContext("2d");
  g.fillStyle = "#fff"; g.fillRect(0, 0, c.width, c.height); // transparent PNGs get a white background in JPEG
  g.drawImage(bmp, 0, 0, c.width, c.height);
  bmp.close?.();
  const blob = await new Promise((res) => c.toBlob(res, "image/jpeg", 0.86));
  if (!blob || (blob.size >= file.size && file.type !== "image/jpeg" && !convert)) return file;
  return new File([blob], (file.name || "image").replace(/\.\w+$/, "") + ".jpg", { type: "image/jpeg" });
}

function readAsBase64(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).replace(/^data:[^,]*,/, ""));
    r.onerror = () => reject(new Error("That file couldn't be read."));
    r.readAsDataURL(file);
  });
}

// put the link where the caret is, with a space either side as needed
function insertLink(url) {
  const s = input.selectionStart ?? input.value.length, e = input.selectionEnd ?? s;
  const before = input.value.slice(0, s), after = input.value.slice(e);
  const text = (before && !/\s$/.test(before) ? " " : "") + url + (/^\s/.test(after) ? "" : " ");
  input.setRangeText(text, s, e, "end");
  autosize();
  input.focus();
}

function setUploading(delta) {
  uploading += delta;
  $("attachBtn").classList.toggle("busy", uploading > 0);
  $("moreBtn").classList.toggle("busy", uploading > 0);
  $("attachBtn").title = uploading > 0 ? "Uploading…" : "Attach an image (or paste / drop one)";
}

async function uploadFiles(files) {
  const list = [...files];
  if (!list.length) return;
  for (let f of list) {
    if (f.type.startsWith("image/")) try { f = await shrinkForUpload(f); } catch {}
    if (!UPLOAD_TYPES.includes(f.type)) { toast(`${f.name || "That file"}: only JPG, PNG, WebP and BMP images can be uploaded`, "error"); continue; }
    if (f.size > UPLOAD_MAX) { toast(`${f.name || "That image"} is over 10 MB`, "error"); continue; }
    setUploading(1);
    toast(list.length > 1 ? `Uploading ${f.name}…` : "Uploading image…", "info");
    try {
      const res = await Go.UploadImage(await readAsBase64(f));
      if (!res?.url) throw new Error("The upload didn't return a link.");
      insertLink(res.url);
      const days = res.expires ? Math.round((Date.parse(res.expires) - Date.now()) / 86400000) : 0;
      toast(days > 0 ? `Image uploaded · link works for ${days} day${days === 1 ? "" : "s"}` : "Image uploaded", "ok");
    } catch (err) {
      toast(String(err?.message ?? err ?? "The upload failed."), "error");
    } finally {
      setUploading(-1);
    }
  }
}

$("attachBtn").onclick = () => {
  if (uploading) return;
  if (!android) { $("attachInput").click(); return; }
  // on the phone: the camera app, or photos already on the phone
  const r = $("attachBtn").getBoundingClientRect();
  showMenu(r.left, r.top - 120, [
    ["Take photo", () => $("cameraInput").click()],
    ["Choose from gallery", () => $("attachInput").click()],
  ]);
};
// phones: a single + button in the message box with everything that used to be its own button
$("moreBtn").onclick = () => {
  const r = $("moreBtn").getBoundingClientRect();
  const items = [];
  if (!uploading) {
    if (android) items.push(["Take photo", () => $("cameraInput").click()], ["Choose from gallery", () => $("attachInput").click()]);
    else items.push(["Attach an image", () => $("attachInput").click()]);
  }
  items.push(["Emoji", () => openEmoji()]);
  showMenu(r.left, r.top - 48 * items.length - 16, items);
};
for (const id of ["attachInput", "cameraInput"])
  $(id).addEventListener("change", (e) => { uploadFiles(e.target.files); e.target.value = ""; });

// Android "Share → IRChatterbox" with photos: the app serves each one at a local URL
on("shareImages", async ({ files }) => {
  const got = [];
  for (const { url, type, name } of files || []) {
    try {
      const blob = await (await fetch(url)).blob();
      got.push(new File([blob], name || "image", { type: type || blob.type }));
    } catch { toast("A shared image couldn't be read", "error"); }
  }
  uploadFiles(got);
});

// pasting a screenshot or a copied image uploads it; pasted text works as before
input.addEventListener("paste", (e) => {
  const files = [...(e.clipboardData?.files || [])].filter((f) => f.type.startsWith("image/"));
  if (!files.length) return;
  e.preventDefault();
  uploadFiles(files);
});

// dropping image files anywhere in the chat or message box uploads them; anything else
// dropped from outside is ignored rather than opened in the window
const isFileDrag = (e) => [...(e.dataTransfer?.types || [])].includes("Files");
document.addEventListener("dragover", (e) => {
  if (!isFileDrag(e)) return;
  e.preventDefault();
  const ok = !!e.target.closest?.("#chatWrap, #composer");
  e.dataTransfer.dropEffect = ok ? "copy" : "none";
  $("composer").classList.toggle("file-drop", ok);
});
document.addEventListener("dragleave", (e) => { if (!e.relatedTarget) $("composer").classList.remove("file-drop"); });
document.addEventListener("drop", (e) => {
  if (!isFileDrag(e)) return;
  e.preventDefault();
  $("composer").classList.remove("file-drop");
  if (e.target.closest?.("#chatWrap, #composer")) uploadFiles(e.dataTransfer.files);
});

function send() {
  const text = input.value;
  if (!text.trim()) return;
  const srv = activeServer(), b = activeBuffer();
  if (!srv || !b) return;
  const lines = text.split(/\r?\n/).filter((l) => l.trim());
  const warnAt = S.cfg.settings.pasteWarnLines ?? 5;
  if (warnAt > 0 && lines.length >= warnAt && b.type !== "server" && !text.trim().startsWith("/")) {
    // big paste: confirm first (sending lots of lines can get you kicked for flooding)
    openPasteConfirm(lines, b.name, (mode) => {
      if (!mode) { input.focus(); return; }
      finishSend(srv, b, text, mode === "join" ? lines.map((l) => l.trim()).join(" ") : lines.join("\n"));
    });
    return;
  }
  finishSend(srv, b, text, text);
}

function finishSend(srv, b, original, text) {
  delete drafts[draftKey(srv, b)];
  persistDrafts();
  S.inputHistory.unshift(original);
  S.inputHistory = S.inputHistory.slice(0, 100);
  S.histPos = -1;
  input.value = "";
  autosize();
  clearTimeout(typingIdle);
  typingSent = { key: "", at: 0, state: "" }; // the message itself ends "typing" for others
  document.getElementById("completeBox")?.remove();
  if (clientCommand(srv, b, text.trim())) return;
  const r = S.reply;
  if (r && r.server === srv.id && r.key === b.key && b.type !== "server") {
    cancelReply();
    Go.SendReply(srv.id, b.name, r.msgid, text);
    return;
  }
  Go.SendInput(srv.id, b.type === "server" ? "" : b.name, text);
}

function clientCommand(srv, b, text) {
  const [cmd, ...rest] = text.split(/\s+/);
  const arg = rest.join(" ");
  switch (cmd.toLowerCase()) {
    case "/clear": b.lines = []; renderChat(); return true;
    case "/settings": openSettings(); return true;
    case "/reload": setTimeout(() => Go.PluginCommands().then((l) => { pluginCommands = l || []; }), 500); return false;
    case "/theme": {
      if (!arg) { addLine(srv, b, { t: Date.now(), type: "info", kind: "help", text: "Usage: /theme auto|light|dark|<theme name>" }); return true; }
      S.cfg.settings.theme = arg.toLowerCase() === "auto" ? "auto" : arg;
      Go.SaveSettings(S.cfg.settings);
      applySettings();
      return true;
    }
    case "/close":
      if (b.type === "server") return true;
      closeBuffer(srv, b);
      return true;
    case "/query": case "/q":
      if (!rest[0]) return true;
      return false;
  }
  return false;
}

// ---------- find ----------
let findText = "";
function openFind() { $("findBar").classList.remove("hidden"); $("findInput").focus(); $("findInput").select(); }
function closeFind() { $("findBar").classList.add("hidden"); $("findInput").value = ""; findText = ""; applyFind(); input.focus(); }
function matchLine(el) {
  const hit = !findText || el.textContent.toLowerCase().includes(findText);
  el.classList.toggle("hidden", !hit);
  return hit;
}
function applyFind() {
  let n = 0;
  for (const el of $("chat").querySelectorAll(".line")) if (matchLine(el) && findText) n++;
  for (const el of $("chat").querySelectorAll(".day,.marker-line")) el.classList.toggle("hidden", !!findText);
  $("findCount").textContent = findText ? `${n} match${n === 1 ? "" : "es"}` : "";
  if (!findText) scrollBottom();
}
$("findInput").addEventListener("input", () => { findText = $("findInput").value.trim().toLowerCase(); applyFind(); });
$("findInput").addEventListener("keydown", (e) => { if (e.key === "Escape") closeFind(); });
$("findAll").addEventListener("click", () => { const q = $("findInput").value.trim(); closeFind(); openSearch(q); });
$("findBtn").onclick = openFind;
$("findClose").onclick = closeFind;

// ---------- quick switcher ----------
function openSwitcher() {
  const ov = $("overlay");
  ov.innerHTML = `<div class="switcher"><input id="swIn" placeholder="Jump to channel, query or server… (type #name to join)"><div class="res" id="swRes"></div></div>`;
  ov.classList.remove("hidden");
  ov.onclick = (e) => { if (e.target === ov) closeOverlay(); };
  const inp = $("swIn");
  let sel = 0, results = [];
  const draw = () => {
    const q = inp.value.trim().toLowerCase();
    results = allBuffers().filter(({ b, srv }) => (b.type === "server" ? srv.cfg.name : b.name).toLowerCase().includes(q))
      .sort((x, y) => (y.b.hl - x.b.hl) || (y.b.unread - x.b.unread));
    if (q.startsWith("#") && !results.some(({ b }) => b.key === q) && activeServer()) results.push({ join: q });
    sel = Math.min(sel, Math.max(0, results.length - 1));
    $("swRes").innerHTML = results.slice(0, 40).map((r, i) => r.join
      ? `<div class="r ${i === sel ? "sel" : ""}" data-i="${i}">Join <b>${esc(r.join)}</b><span class="s">${esc(activeServer().cfg.name)}</span></div>`
      : `<div class="r ${i === sel ? "sel" : ""}" data-i="${i}"><span>${r.b.type === "channel" ? "#" : r.b.type === "query" ? "@" : "◆"}</span>${esc(r.b.type === "server" ? r.srv.cfg.name : r.b.name.replace(/^#/, ""))}${r.b.unread ? ` <span class="badge ${r.b.hl ? "hl" : ""}">${r.b.unread}</span>` : ""}<span class="s">${esc(r.srv.cfg.name)}</span></div>`).join("");
  };
  const go = (i) => {
    const r = results[i];
    if (!r) return;
    closeOverlay();
    if (r.join) Go.SendInput(activeServer().id, "", "/join " + r.join);
    else select(r.srv.id, r.b.key);
  };
  inp.addEventListener("input", () => { sel = 0; draw(); });
  inp.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { e.preventDefault(); sel = Math.min(sel + 1, results.length - 1); draw(); }
    if (e.key === "ArrowUp") { e.preventDefault(); sel = Math.max(sel - 1, 0); draw(); }
    if (e.key === "Enter") go(sel);
    if (e.key === "Escape") closeOverlay();
  });
  $("swRes").onclick = (e) => { const r = e.target.closest("[data-i]"); if (r) go(+r.dataset.i); };
  draw();
  inp.focus();
}
$("jumpBtn").onclick = openSwitcher;

// ---------- global keys ----------
document.addEventListener("keydown", (e) => {
  const overlayOpen = !$("overlay").classList.contains("hidden");
  if (e.key === "Escape") { if (overlayOpen) closeOverlay(); hideMenu(); return; }
  if (overlayOpen) return;
  if (e.ctrlKey && e.key.toLowerCase() === "k") { e.preventDefault(); openSwitcher(); }
  else if (e.ctrlKey && e.shiftKey && e.key.toLowerCase() === "f") { e.preventDefault(); openSearch(); }
  else if (e.ctrlKey && e.key.toLowerCase() === "f") { e.preventDefault(); openFind(); }
  else if (e.altKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) { e.preventDefault(); cycleBuffer(e.key === "ArrowDown" ? 1 : -1); }
  else if (e.ctrlKey && e.key === "Tab") { e.preventDefault(); cycleBuffer(e.shiftKey ? -1 : 1); }
  else if (e.altKey && e.key.toLowerCase() === "a") { e.preventDefault(); cycleBuffer(1, true); }
  else if (e.ctrlKey && e.key === "End") { e.preventDefault(); scrollBottom(); }
  else if (e.key === "PageUp" || e.key === "PageDown") { const c = $("chat"); c.scrollTop += (e.key === "PageUp" ? -1 : 1) * c.clientHeight * 0.9; }
  else if (e.ctrlKey && e.key === ",") { e.preventDefault(); openSettings(); }
  else if (e.ctrlKey && e.key.toLowerCase() === "e") { e.preventDefault(); openEmoji(); }
  else if (e.key === "F11") { e.preventDefault(); Go.ToggleFullscreen(); }
  else if (e.key === "F6" && isSplit()) { e.preventDefault(); focusPane((S.ui.focus + (e.shiftKey ? LAYOUTS[layout()] - 1 : 1)) % LAYOUTS[layout()]); }
  else if (!e.ctrlKey && !e.altKey && !e.metaKey && e.key.length === 1 && document.activeElement?.tagName !== "INPUT" && document.activeElement?.tagName !== "TEXTAREA") input.focus();
});

// ---------- window focus ----------
function setFocus(f) {
  if (S.focused === f) return;
  S.focused = f;
  Go.SetFocus(f);
  const b = activeBuffer();
  if (!b) return;
  if (!f) {
    b.markPending = true; // alt-tabbed away: the next message gets the line
  } else {
    b.markPending = false;
    if (b.unread || b.hl) { b.unread = 0; b.hl = 0; b.activity = false; scheduleTree(); }
    sendRead(activeServer(), b);
    renderPanes();
  }
}
window.addEventListener("focus", () => setFocus(true));
window.addEventListener("blur", () => setFocus(false));
document.addEventListener("visibilitychange", () => setFocus(!document.hidden && document.hasFocus()));

// ---------- layout ----------
function setupResizer(id, varName, side) {
  const r = $(id);
  r.addEventListener("mousedown", (e) => {
    e.preventDefault();
    const startX = e.clientX;
    const startW = parseInt(getComputedStyle(document.documentElement).getPropertyValue(varName));
    const move = (ev) => {
      const w = Math.max(160, Math.min(480, startW + (ev.clientX - startX) * (side === "left" ? 1 : -1)));
      document.documentElement.style.setProperty(varName, w + "px");
      S.ui[varName] = w;
    };
    const up = () => { removeEventListener("mousemove", move); removeEventListener("mouseup", up); saveUi(); };
    addEventListener("mousemove", move);
    addEventListener("mouseup", up);
  });
}
setupResizer("sideResizer", "--side-w", "left");
setupResizer("userResizer", "--users-w", "right");
for (const v of ["--side-w", "--users-w", "--nick-col"]) if (S.ui[v]) document.documentElement.style.setProperty(v, S.ui[v] + "px");

// Draggable divider between the nick column and the message column.
function positionNickHandle() {
  const h = $("nickHandle");
  if (stackedLayout()) { h.style.display = "none"; return; }
  const who = $("chat").querySelector(".line:not(.hidden) .who");
  if (!who) { h.style.display = "none"; return; }
  h.style.display = "";
  const wrap = $("chatWrap").getBoundingClientRect();
  h.style.left = who.getBoundingClientRect().right - wrap.left + "px";
}
function setNickCol(w) {
  w = Math.round(Math.max(40, Math.min(400, w)));
  document.documentElement.style.setProperty("--nick-col", w + "px");
  S.ui["--nick-col"] = w;
  positionNickHandle();
}
$("nickHandle").addEventListener("mousedown", (e) => {
  e.preventDefault();
  const startX = e.clientX;
  const startW = parseInt(getComputedStyle(document.documentElement).getPropertyValue("--nick-col")) || 132;
  $("nickHandle").classList.add("drag");
  document.body.classList.add("col-resizing");
  const move = (ev) => setNickCol(startW + ev.clientX - startX);
  const up = () => {
    removeEventListener("mousemove", move); removeEventListener("mouseup", up);
    $("nickHandle").classList.remove("drag");
    document.body.classList.remove("col-resizing");
    saveUi();
  };
  addEventListener("mousemove", move);
  addEventListener("mouseup", up);
});
// double-click: fit the longest nick currently shown
$("nickHandle").addEventListener("dblclick", () => {
  const probe = document.createElement("span");
  probe.style.cssText = "position:absolute;visibility:hidden;white-space:nowrap;font-weight:600;font-size:var(--fs);font-family:var(--chat-font)";
  document.body.appendChild(probe);
  let max = 40;
  for (const el of $("chat").querySelectorAll(".line .who")) {
    probe.innerHTML = el.innerHTML;
    max = Math.max(max, probe.offsetWidth);
  }
  probe.remove();
  setNickCol(max + 12); // + padding-right
  saveUi();
});
new ResizeObserver(positionNickHandle).observe($("chatWrap"));
// When the chat gets shorter (the phone keyboard opening, the window being resized), keep the
// newest lines in view if that's where you were reading.
new ResizeObserver(() => { if (stick) scrollBottom(); }).observe($("chat"));

$("usersBtn").onclick = () => { S.ui.users = S.ui.users === false; saveUi(); renderHeader(); renderUsers(); };
$("settingsBtn").onclick = () => openSettings();
$("addServerBtn").onclick = () => editServer();

export function applySettings() {
  const set = S.cfg.settings;
  const root = document.documentElement.style;
  root.setProperty("--fs", (set.fontSize || 14) + "px");
  root.setProperty("--chat-font", set.fontFamily ? `"${set.fontFamily}", var(--font)` : "var(--theme-chat-font, var(--font))");
  setNickOverrides(S.cfg.nickColors);
  const theme = resolveTheme(set);
  applyTheme(theme);
  setNickContrast(getComputedStyle(document.documentElement).getPropertyValue("--bg").trim(), set.nickContrast);
  // fixed-width timestamp column so the nick divider lines up on every row
  const probe = document.createElement("span");
  probe.style.cssText = "position:absolute;visibility:hidden;white-space:nowrap;font-size:calc(var(--fs) * .82);font-variant-numeric:tabular-nums;font-family:var(--chat-font)";
  probe.textContent = fmtTime(new Date(2000, 0, 1, 22, 58, 58).getTime());
  document.body.appendChild(probe);
  root.setProperty("--ts-col", probe.offsetWidth + 2 + "px");
  probe.remove();
  if (typeof positionNickHandle === "function") requestAnimationFrame(positionNickHandle);
  const box = $("input");
  box.spellcheck = set.spellCheck !== false;
  box.lang = document.documentElement.lang || "en";
  afterTheme();
}

export function renderAll() {
  renderTree(); renderHeader(); renderChat(); renderUsers(); renderStatus(); renderMe(); renderPanes();
}

// Called by modals after the config changed.
export async function reloadConfig() {
  S.cfg = await Go.GetConfig();
  setCustomThemes(await Go.GetThemes());
  for (const sc of S.cfg.servers) {
    if (S.byId[sc.id]) S.byId[sc.id].cfg = sc;
    else { const srv = makeServer(sc); S.servers.push(srv); S.byId[sc.id] = srv; }
  }
  applySettings();
  renderAll();
}

export function editServer(cfg) {
  openServerEditor(cfg, async (saved, connect) => {
    await reloadConfig();
    if (connect) {
      const srv = S.byId[saved.id];
      if (srv && srv.status !== "disconnected") await Go.SendInput(saved.id, "", "/reconnect");
      else await Go.Connect(saved.id);
      select(saved.id, "");
    } else if (!S.active.server) select(saved.id, "");
    toast(connect ? `Saved ${saved.name} — connecting…` : `Saved ${saved.name}`, "ok");
  });
}

document.querySelector(".brand .logo").outerHTML = `<img class="logo-img" src="${logoUrl}" alt="">`;

// ---------- boot ----------
async function boot() {
  await initApi();
  await detectOS();
  // frontend errors go to app.log too (Settings → About → Open logs)
  if (!android) {
    addEventListener("error", (e) => Go.LogUI(`${e.message} at ${e.filename}:${e.lineno}`));
    addEventListener("unhandledrejection", (e) => Go.LogUI("unhandled promise: " + (e.reason?.stack || e.reason)));
  }
  initMobile({ tabComplete, hideMenu, closeOverlay, closeFind });
  S.icons = (await Go.GetChannelIcons()) || {};
  setSidebarMini(S.ui.sbMini);
  $("sbToggle").onclick = () => setSidebarMini(!S.ui.sbMini);
  $("meAvatar").title = "Change your avatar";
  $("meAvatar").onclick = () => pickIcon("_me", "avatar", "your avatar");
  $("meAvatar").oncontextmenu = (e) => {
    e.preventDefault();
    showMenu(e.clientX, e.clientY, [["Change avatar…", () => pickIcon("_me", "avatar", "your avatar")]]
      .concat(myAvatar() ? [["Remove avatar", () => Go.SetChannelIcon("_me", "avatar", "")]] : []));
  };
  S.cfg = await Go.GetConfig();
  await setLanguage(S.cfg.settings.language || "");
  setCustomThemes(await Go.GetThemes());
  applySettings();
  // Follow Windows light/dark. Besides the change event, re-check when the window comes back
  // and once a minute: the web view can miss the event while the app is hidden in the tray.
  let lastDark = systemIsDark();
  const recheckTheme = (force) => {
    if (force === true || systemIsDark() !== lastDark) { lastDark = systemIsDark(); applySettings(); }
  };
  onSystemThemeChange(() => recheckTheme(true));
  addEventListener("focus", recheckTheme);
  document.addEventListener("visibilitychange", recheckTheme);
  setInterval(recheckTheme, 60000);

  for (const sc of S.cfg.servers) {
    const srv = makeServer(sc);
    S.servers.push(srv);
    S.byId[sc.id] = srv;
  }
  const state = (await Go.GetState()) || [];
  for (const st of state) {
    const srv = S.byId[st.id];
    if (!srv) continue;
    Object.assign(srv, { status: st.status, nick: st.nick || srv.nick, network: st.network });
    for (const ch of st.channels || []) {
      const b = ensureBuffer(srv, ch.name, "channel");
      Object.assign(b, { topic: ch.topic, modes: ch.modes, users: ch.users || [], joined: true });
    }
  }
  on("irc", onIrc);
  on("confirmClose", (d) => openQuitConfirm(d?.connected, d?.bouncer));
  on("playSound", (d) => playSound(d.sound, d.volume));
  on("updateAvailable", (info) => showUpdateBar(info));
  on("localUpdateReady", () => {
    const bar = $("updateBar");
    bar.innerHTML = `<span class="ub-text"><b>A new build of IRChatterbox is ready.</b> Restart to start using it.</span>
      <button class="btn sm primary" id="lbApply">Restart to update</button><button class="icon-btn" id="lbClose" title="Later">✕</button>`;
    bar.classList.remove("hidden");
    $("lbClose").onclick = () => bar.classList.add("hidden");
    $("lbApply").onclick = async () => {
      const err = await Go.ApplyLocalUpdate();
      if (err) toast(err, "error");
      else bar.querySelector(".ub-text").textContent = "Restarting…";
    };
  });
  on("updateProgress", (d) => {
    const bar = document.querySelector("#updateBar .ub-progress i");
    if (bar) bar.style.width = d.pct + "%";
  });
  Go.PluginCommands().then((list) => { pluginCommands = list || []; });
  on("config", (cfg) => { S.cfg = cfg; applySettings(); renderAll(); });
  on("dcc", onDCC);
  on("chanlist", onChanList);
  on("chanlistOpen", (x) => { const srv = S.byId[x.server]; if (srv) openChannelList(srv, x.filter || ""); });
  on("dccPick", (x) => { const srv = S.byId[x.server]; if (srv) dccPickAndSend(srv, x.nick); });
  on("icons", (icons) => { S.icons = icons || {}; renderTree(); renderHeader(); renderMe(); });
  on("focusBuffer", ({ server, buffer }) => {
    const srv = S.byId[server];
    if (!srv) return;
    const b = resolveBuffer(srv, buffer);
    select(server, b.key);
  });

  buildPanes();
  // open the channel chosen with "Show at startup", otherwise where you left off
  const sv = S.cfg.startView;
  const last = sv?.server && S.byId[sv.server] ? { server: sv.server, name: sv.buffer || "" } : S.ui.last;
  if (last && S.byId[last.server]) {
    const srv = S.byId[last.server];
    const b = last.name ? ensureBuffer(srv, last.name) : srv.buffers.get("");
    select(srv.id, b.key);
  } else if (S.servers[0]) select(S.servers[0].id, "");
  else renderAll();

  Go.SetFocus(document.hasFocus());
  showCrashNotice();
  await Go.AutoConnect();

  // First run only: open the server editor when there's nothing to connect to yet.
  if (!S.servers.some((s) => s.cfg.nick && s.cfg.host)) editServer(S.servers[0]?.cfg);
}

boot();
