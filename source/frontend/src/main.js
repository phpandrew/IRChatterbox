import { Go, on, initApi, setTitle } from "./api.js";
import { esc, ircToHtml, nickColor, baseNick, setNickOverrides, findUrls, mediaFor, mentionRegex, stripCodes } from "./format.js";
import { applyTheme, resolveTheme, setCustomThemes, onSystemThemeChange } from "./themes.js";
import logoUrl from "./assets/logo.png";
import { toggleEmojiPicker } from "./emoji.js";
import { openServerEditor, openSettings, openPrompt, openColorPicker, openQuitConfirm } from "./modals.js";

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
  }
  return b;
}

function isChannel(srv, name) { return !!name && "#&!+".includes(name[0]); }
function activeServer() { return S.byId[S.active.server]; }
function activeBuffer() { const s = activeServer(); return s?.buffers.get(S.active.key); }

async function loadHistory(srv, b) {
  b.history = true;
  const lines = (await Go.GetHistory(srv.id, b.name)) || [];
  const old = lines.filter((ev) => ev.time < b.created).map((ev) => ({ ...evToLine(ev), history: true, hl: false }));
  if (!old.length) return;
  b.lines = old.concat(b.lines);
  if (isActive(srv, b)) renderChat();
}

function isActive(srv, b) { return S.active.server === srv.id && S.active.key === b.key; }

function evToLine(ev) {
  return { t: ev.time, kind: ev.kind || ev.type, type: ev.type, nick: ev.nick, prefix: ev.prefix, text: ev.text || "", self: ev.self, hl: ev.highlight, extra: ev.extra };
}

function resolveBuffer(srv, name) {
  if (name === "*active*") {
    if (S.active.server === srv.id) return srv.buffers.get(S.active.key) || srv.buffers.get("");
    return srv.buffers.get("");
  }
  return ensureBuffer(srv, name);
}

function addLine(srv, b, line) {
  b.lines.push(line);
  if (b.lines.length > MAX_LINES) {
    const drop = b.lines.length - MAX_LINES;
    b.lines.splice(0, drop);
  }
  const counts = ["privmsg", "action", "notice"].includes(line.kind) && !line.self;
  const visible = isActive(srv, b) && S.focused;
  // Marker line: the first message that arrives after you stopped looking at this buffer
  // (switched away or alt-tabbed) gets the divider above it.
  let moveMarker = false;
  if (counts && !visible && b.markPending) {
    b.marker = line;
    b.markPending = false;
    moveMarker = true;
  }
  if (isActive(srv, b)) {
    if (moveMarker) $("chat").querySelector(".marker-line")?.remove();
    appendLineDom(b, line);
  }
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
      addLine(srv, b, evToLine(ev));
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
  if (S.active.server === srv.id && S.active.key === key) select(srv.id, "");
  scheduleTree();
}

// ---------- selection ----------
function select(serverId, key) {
  const prev = activeBuffer();
  if (prev && !(S.active.server === serverId && S.active.key === key)) {
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
  b.markPending = false; // you're reading it now
  b.unread = 0; b.hl = 0; b.activity = false;
  scheduleTree();
  $("input").focus();
}

function allBuffers() {
  const out = [];
  for (const srv of S.servers) {
    for (const b of sortedBuffers(srv)) out.push({ srv, b });
  }
  return out;
}

function sortedBuffers(srv) {
  const list = [...srv.buffers.values()];
  const rank = (b) => (b.type === "server" ? 0 : b.type === "channel" ? 1 : 2);
  return list.sort((a, b) => rank(a) - rank(b) || a.key.localeCompare(b.key));
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
};

function renderTree() {
  const tree = $("tree");
  let html = "";
  for (const srv of S.servers) {
    let aggU = 0, aggH = 0;
    let kids = "";
    for (const b of sortedBuffers(srv)) {
      aggU += b.unread; aggH += b.hl;
      const active = isActive(srv, b);
      const cls = ["buf", active && "active", b.activity && "activity", b.hl && "hl", b.type === "channel" && !b.joined && "parted"].filter(Boolean).join(" ");
      const ico = b.type === "server" ? ICONS.server : b.type === "channel" ? "#" : ICONS.query;
      const label = b.type === "server" ? "Server status" : b.type === "channel" ? esc(b.name.replace(/^#/, "")) : esc(b.name);
      const badge = b.hl ? `<span class="badge hl">${b.hl}</span>` : b.unread ? `<span class="badge">${b.unread > 999 ? "999+" : b.unread}</span>` : "";
      const x = b.type !== "server" ? `<span class="x" data-close="1" title="${b.type === "channel" ? "Leave" : "Close"}">✕</span>` : "";
      kids += `<div class="${cls}" data-s="${srv.id}" data-k="${esc(b.key)}"><span class="ico">${ico}</span><span class="nm">${label}</span>${badge}${x}</div>`;
    }
    const stTxt = { connected: "Connected", connecting: "Connecting…", disconnected: "Disconnected" }[srv.status];
    const agg = srv.collapsed ? (aggH ? `<span class="badge hl">${aggH}</span>` : aggU ? `<span class="badge">${aggU}</span>` : "") : "";
    html += `<div class="srv ${srv.collapsed ? "collapsed" : ""}" data-srv="${srv.id}">
      <div class="srv-head" data-head="${srv.id}">${ICONS.chev}
        <div style="flex:1;min-width:0"><div class="srv-name">${esc(srv.cfg.name || srv.cfg.host)}</div>
        <div class="srv-state"><i class="dot ${srv.status}"></i>${stTxt}${srv.status === "connected" && srv.nick ? " as " + esc(srv.nick) : ""}</div></div>${agg}
      </div>
      <div class="srv-kids">${kids}</div></div>`;
  }
  if (!S.servers.length) html = `<div class="empty" style="padding:30px 10px">No servers yet.</div>`;
  tree.innerHTML = html;
  updateTitle();
}

function updateTitle() {
  const srv = activeServer(), b = activeBuffer();
  let total = 0;
  for (const s of S.servers) for (const x of s.buffers.values()) total += x.hl;
  const where = b ? (b.type === "server" ? srv.cfg.name : `${b.name} · ${srv.network || srv.cfg.name}`) : "";
  setTitle(`${total ? `(${total}) ` : ""}IRChatterbox${where ? " — " + where : ""}`);
}

$("tree").addEventListener("click", (e) => {
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
});

function closeBuffer(srv, b) {
  Go.CloseBuffer(srv.id, b.name);
  closeBufferLocal(srv, b.key);
}

$("tree").addEventListener("contextmenu", (e) => {
  e.preventDefault();
  const head = e.target.closest("[data-head]");
  const row = e.target.closest(".buf");
  if (head) return serverMenu(S.byId[head.dataset.head], e);
  if (row) {
    const srv = S.byId[row.dataset.s];
    const b = srv.buffers.get(row.dataset.k);
    if (b.type === "server") return serverMenu(srv, e);
    const items = [];
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
    items.push("-");
    items.push(["Mark as read", () => { b.unread = b.hl = 0; b.activity = false; scheduleTree(); }]);
    items.push(["Clear buffer", () => { b.lines = []; if (isActive(srv, b)) renderChat(); }]);
    items.push("-");
    items.push([b.type === "channel" ? "Leave & close" : "Close", () => closeBuffer(srv, b), "danger"]);
    showMenu(e.clientX, e.clientY, items, b.name);
  }
});

function serverMenu(srv, e) {
  const items = [];
  if (srv.status === "disconnected") items.push(["Connect", () => Go.Connect(srv.id)]);
  else items.push(["Disconnect", () => Go.Disconnect(srv.id)]);
  items.push(["Reconnect", () => Go.SendInput(srv.id, "", "/reconnect")]);
  items.push(["Join channel…", () => openPrompt("Join channel", "#channel", "", (v) => v && Go.SendInput(srv.id, "", "/join " + v))]);
  items.push("-");
  items.push(["Edit server…", () => editServer(srv.cfg)]);
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
  showMenu(e.clientX, e.clientY, items, srv.cfg.name);
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
  if (!srv || !b) { name.textContent = ""; sub.textContent = ""; topic.innerHTML = ""; meta.innerHTML = ""; return; }
  const net = srv.network || srv.cfg.name;
  if (b.type === "channel") {
    name.innerHTML = `<span class="hash">#</span>${esc(b.name.replace(/^#/, ""))}`;
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
    name.innerHTML = `<span style="color:${nickColor(b.name)}">${esc(b.name)}</span>`;
    sub.textContent = `Private conversation · ${net}`;
    topic.innerHTML = "";
    meta.innerHTML = "";
  } else {
    name.textContent = srv.cfg.name || srv.cfg.host;
    sub.textContent = `${srv.cfg.host}:${srv.cfg.port}${srv.cfg.tls ? " · TLS" : ""}`;
    topic.classList.remove("editable");
    topic.title = "";
    topic.innerHTML = `<span class="muted">Server messages, MOTD and notices</span>`;
    meta.innerHTML = `<span><i class="dot ${srv.status}"></i> <b>${srv.status}</b></span>`;
  }
  $("input").placeholder = b.type === "server" ? `Type a /command for ${srv.cfg.name}` : `Message ${b.name}`;
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
  $("meAvatar").textContent = (nick[0] || "?").toUpperCase();
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

function lineEl(srv, b, line) {
  const el = document.createElement("div");
  const isEv = line.type === "event";
  el.className = "line " + [line.kind, isEv ? "ev" : "", line.type === "info" ? "info" : "", line.self ? "self" : "", line.hl ? "hl" : "", line.history ? "history" : ""].join(" ");
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
  el.innerHTML = `<span class="ts">${fmtTime(line.t)}</span><span class="who"${whoStyle} data-nick="${esc(line.nick || "")}" title="${esc(line.nick || "")}">${who}</span><span class="msg">${msg}</span>`;
  if (line.type === "msg") addMedia(el, line.text);
  return el;
}

function addMedia(el, text) {
  const set = S.cfg.settings;
  if (!set.inlineImages && !set.inlineYouTube) return;
  const seen = new Set();
  for (const url of findUrls(text)) {
    const m = mediaFor(url);
    if (!m || seen.has(m.src || m.id)) continue;
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
    } else continue;
    el.appendChild(document.createElement("span"));
    el.appendChild(document.createElement("span"));
    el.appendChild(wrap);
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
  if (!srv || !b) {
    chat.innerHTML = `<div class="empty"><img src="${logoUrl}" width="96" alt="" style="margin-bottom:12px"><h2>Welcome to IRChatterbox</h2><p>Add a server to get started.</p><p><button class="btn primary" id="emptyAdd">Add server</button></p></div>`;
    $("emptyAdd").onclick = () => editServer();
    return;
  }
  const frag = document.createDocumentFragment();
  let markerEl = null;
  b.lines.forEach((line) => {
    const day = new Date(line.t).toDateString();
    if (day !== lastRenderedDay) {
      lastRenderedDay = day;
      const d = document.createElement("div"); d.className = "day"; d.textContent = dayLabel(line.t); frag.appendChild(d);
    }
    if (line === b.marker && S.cfg.settings.markerLine !== false) frag.appendChild((markerEl = markerLineEl()));
    frag.appendChild(lineEl(srv, b, line));
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
  const el = lineEl(activeServer(), b, line);
  if (line === b.marker && S.cfg.settings.markerLine !== false) chat.appendChild(markerLineEl());
  chat.appendChild(el);
  while (chat.childElementCount > MAX_LINES + 50) chat.firstElementChild.remove();
  if (findText) matchLine(el);
  if (chat.childElementCount < 4) requestAnimationFrame(positionNickHandle);
  if (atBottom || line.self) { stick = true; scrollBottom(); }
  else if (!line.history) { newBelow++; updateToLatest(); }
}

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
  }
});
document.addEventListener("click", (e) => {
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

function userMenu(nick, e) {
  const srv = activeServer(), b = activeBuffer();
  if (!srv) return;
  const send = (cmd) => Go.SendInput(srv.id, b?.name || "", cmd);
  const items = [
    ["Message", () => openQuery(nick)],
    ["Whois", () => send("/whois " + nick)],
    ["Mention", () => insertMention(nick)],
  ];
  if (b?.type === "channel") {
    const u = b.users.find((x) => x.nick === nick);
    const p = u?.prefix || "";
    items.push("-");
    items.push(p.includes("@") ? ["Deop", () => send("/deop " + nick)] : ["Op", () => send("/op " + nick)]);
    items.push(p.includes("+") ? ["Devoice", () => send("/devoice " + nick)] : ["Voice", () => send("/voice " + nick)]);
    items.push(["Kick…", () => openPrompt(`Kick ${nick}`, "Reason", "", (r) => send(`/kick ${nick} ${r || nick}`), "Kick")]);
    items.push(["Ban", () => send("/ban " + nick)]);
    items.push(["Kick + ban…", () => openPrompt(`Kick and ban ${nick}`, "Reason", "", (r) => send(`/kb ${nick} ${r || "Banned"}`), "Kick + ban")], "danger");
  }
  items.push("-");
  const custom = S.cfg.nickColors?.[nick.toLowerCase()] || S.cfg.nickColors?.[baseNick(nick)] || "";
  items.push(["Set nick colour…", () => openColorPicker(nick, custom, (c) => Go.SetNickColor(nick, c))]);
  if (custom) items.push(["Reset nick colour", () => Go.SetNickColor(nick, "")]);
  items.push(["Ignore", () => send("/ignore " + nick), "danger"]);
  showMenu(e.clientX, e.clientY, items.filter((x) => x !== "danger"), nick);
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
export function showMenu(x, y, items, header) {
  const m = $("menu");
  m.innerHTML = (header ? `<div class="mh">${esc(header)}</div>` : "") + items.map((it, i) =>
    it === "-" ? `<div class="sep"></div>` : `<button class="mi ${it[2] || ""}${it.length > 3 ? " chk" : ""}${it[3] ? " on" : ""}" data-i="${i}">${esc(it[0])}</button>`).join("");
  m.classList.remove("hidden");
  const r = m.getBoundingClientRect();
  m.style.left = Math.min(x, innerWidth - r.width - 8) + "px";
  m.style.top = Math.min(y, innerHeight - r.height - 8) + "px";
  m.onclick = (e) => {
    const b = e.target.closest("[data-i]");
    if (!b) return;
    hideMenu();
    items[+b.dataset.i][1]();
  };
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
input.addEventListener("input", () => { autosize(); updateCompletions(); tabState = null; });

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
    compItems = COMMANDS.filter(([c]) => c.startsWith(q)).slice(0, 12).map(([c, d]) => ({
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

function send() {
  const text = input.value;
  if (!text.trim()) return;
  const srv = activeServer(), b = activeBuffer();
  if (!srv || !b) return;
  S.inputHistory.unshift(text);
  S.inputHistory = S.inputHistory.slice(0, 100);
  S.histPos = -1;
  input.value = "";
  autosize();
  document.getElementById("completeBox")?.remove();
  if (clientCommand(srv, b, text.trim())) return;
  Go.SendInput(srv.id, b.type === "server" ? "" : b.name, text);
}

function clientCommand(srv, b, text) {
  const [cmd, ...rest] = text.split(/\s+/);
  const arg = rest.join(" ");
  switch (cmd.toLowerCase()) {
    case "/clear": b.lines = []; renderChat(); return true;
    case "/settings": openSettings(); return true;
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
  else if (e.ctrlKey && e.key.toLowerCase() === "f") { e.preventDefault(); openFind(); }
  else if (e.altKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) { e.preventDefault(); cycleBuffer(e.key === "ArrowDown" ? 1 : -1); }
  else if (e.ctrlKey && e.key === "Tab") { e.preventDefault(); cycleBuffer(e.shiftKey ? -1 : 1); }
  else if (e.altKey && e.key.toLowerCase() === "a") { e.preventDefault(); cycleBuffer(1, true); }
  else if (e.ctrlKey && e.key === "End") { e.preventDefault(); scrollBottom(); }
  else if (e.key === "PageUp" || e.key === "PageDown") { const c = $("chat"); c.scrollTop += (e.key === "PageUp" ? -1 : 1) * c.clientHeight * 0.9; }
  else if (e.ctrlKey && e.key === ",") { e.preventDefault(); openSettings(); }
  else if (e.ctrlKey && e.key.toLowerCase() === "e") { e.preventDefault(); openEmoji(); }
  else if (e.key === "F11") { e.preventDefault(); Go.ToggleFullscreen(); }
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

$("usersBtn").onclick = () => { S.ui.users = S.ui.users === false; saveUi(); renderHeader(); renderUsers(); };
$("settingsBtn").onclick = () => openSettings();
$("addServerBtn").onclick = () => editServer();

export function applySettings() {
  const set = S.cfg.settings;
  const root = document.documentElement.style;
  root.setProperty("--fs", (set.fontSize || 14) + "px");
  root.setProperty("--chat-font", set.fontFamily ? `"${set.fontFamily}", var(--font)` : "var(--font)");
  setNickOverrides(S.cfg.nickColors);
  applyTheme(resolveTheme(set));
  // fixed-width timestamp column so the nick divider lines up on every row
  const probe = document.createElement("span");
  probe.style.cssText = "position:absolute;visibility:hidden;white-space:nowrap;font-size:calc(var(--fs) * .82);font-variant-numeric:tabular-nums;font-family:var(--chat-font)";
  probe.textContent = fmtTime(new Date(2000, 0, 1, 22, 58, 58).getTime());
  document.body.appendChild(probe);
  root.setProperty("--ts-col", probe.offsetWidth + 2 + "px");
  probe.remove();
  if (typeof positionNickHandle === "function") requestAnimationFrame(positionNickHandle);
}

export function renderAll() {
  renderTree(); renderHeader(); renderChat(); renderUsers(); renderStatus(); renderMe();
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
  S.cfg = await Go.GetConfig();
  setCustomThemes(await Go.GetThemes());
  applySettings();
  onSystemThemeChange(() => applySettings());

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
  on("confirmClose", (d) => openQuitConfirm(d?.connected));
  on("config", (cfg) => { S.cfg = cfg; applySettings(); renderAll(); });
  on("focusBuffer", ({ server, buffer }) => {
    const srv = S.byId[server];
    if (!srv) return;
    const b = resolveBuffer(srv, buffer);
    select(server, b.key);
  });

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
  await Go.AutoConnect();

  // First run only: open the server editor when there's nothing to connect to yet.
  if (!S.servers.some((s) => s.cfg.nick && s.cfg.host)) editServer(S.servers[0]?.cfg);
}

boot();
