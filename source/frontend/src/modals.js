import { Go } from "./api.js";
import wordmarkLight from "./assets/wordmark-light.png";
import wordmarkDark from "./assets/wordmark-dark.png";
import githubIcon from "./assets/github.png";
import { BUILTIN_SOUNDS, previewSound } from "./sounds.js";
import { showUpdateBar } from "./main.js";

const REPO = "https://github.com/phpandrew/IRChatterbox";
import { S, closeOverlay, applySettings, reloadConfig, toast } from "./main.js";
import { esc, nickColor } from "./format.js";
import { THEME_VARS, BUILTIN, allThemes, applyTheme, getTheme, resolveTheme, setCustomThemes } from "./themes.js";

const $ = (id) => document.getElementById(id);

function open(html, cls = "", sticky = false) {
  const ov = $("overlay");
  ov.innerHTML = `<div class="modal ${cls}">${html}</div>`;
  ov.classList.remove("hidden");
  // sticky dialogs (forms) don't close when you click outside them, so typed data isn't lost
  ov.onclick = (e) => { if (e.target === ov && !sticky) closeOverlay(); };
  return ov.firstElementChild;
}

// a settings group: title, one-line description, then its controls
const card = (title, desc, inner) =>
  `<section class="set-card"><div class="set-head"><h3>${title}</h3>${desc ? `<p>${desc}</p>` : ""}</div><div class="set-body">${inner}</div></section>`;
const field = (label, inner, hint = "", full = false) =>
  `<div class="field${full ? " full" : ""}"><label>${label}</label>${inner}${hint ? `<div class="hint">${hint}</div>` : ""}</div>`;
const text = (id, v, ph = "", type = "text") => `<input type="${type}" id="${id}" value="${esc(v ?? "")}" placeholder="${esc(ph)}">`;
const check = (id, v, label) => `<label class="check"><input type="checkbox" id="${id}" ${v ? "checked" : ""}> ${label}</label>`;
const val = (id) => $(id).value.trim();
const on = (id) => $(id).checked;
const lines = (id) => $(id).value.split("\n").map((s) => s.trim()).filter(Boolean);

// ---------- simple prompt ----------
export function openPrompt(title, placeholder, value, onOk, okLabel = "OK") {
  const m = open(`<div class="modal-head">${esc(title)}</div>
    <div class="modal-body">${value === null ? "" : `<div class="field"><input type="text" id="pIn" value="${esc(value)}" placeholder="${esc(placeholder)}"></div>`}</div>
    <div class="modal-foot"><button class="btn" id="pCancel">Cancel</button><button class="btn primary" id="pOk">${esc(okLabel)}</button></div>`, "sm");
  const ok = () => { const v = $("pIn")?.value.trim(); closeOverlay(); onOk(v); };
  $("pCancel").onclick = closeOverlay;
  $("pOk").onclick = ok;
  $("pIn")?.addEventListener("keydown", (e) => { if (e.key === "Enter") ok(); });
  ($("pIn") || $("pOk")).focus();
  return m;
}

export function openColorPicker(nick, current, onPick) {
  const preset = ["#e5484d", "#f76b15", "#e2b714", "#30a46c", "#12a594", "#05a2c2", "#3e63dd", "#8e4ec6", "#d6409f", "#ad7f58"];
  open(`<div class="modal-head">Colour for <span style="margin-left:6px;color:${nickColor(nick)}" id="cpName">${esc(nick)}</span></div>
    <div class="modal-body">
      <div class="chips" style="margin-bottom:12px">${preset.map((c) => `<button class="swatch" data-c="${c}" style="width:28px;height:28px;border-radius:7px;background:${c}"></button>`).join("")}</div>
      <div class="tv"><input type="color" id="cpColor" value="${current || "#4cc2ff"}"><input type="text" id="cpHex" value="${esc(current || "")}" placeholder="#rrggbb"><span class="muted">Same as <code>/color ${esc(nick)} #hex</code></span></div>
    </div>
    <div class="modal-foot"><button class="btn left" id="cpReset">Reset to automatic</button><button class="btn" id="cpCancel">Cancel</button><button class="btn primary" id="cpOk">Save</button></div>`, "sm");
  const set = (c) => { $("cpColor").value = c; $("cpHex").value = c; $("cpName").style.color = c; };
  document.querySelectorAll("[data-c]").forEach((b) => (b.onclick = () => set(b.dataset.c)));
  $("cpColor").oninput = () => set($("cpColor").value);
  $("cpHex").oninput = () => { if (/^#[0-9a-f]{6}$/i.test($("cpHex").value)) set($("cpHex").value); };
  $("cpCancel").onclick = closeOverlay;
  $("cpReset").onclick = () => { closeOverlay(); onPick(""); };
  const pick = () => { const c = $("cpHex").value.trim() || $("cpColor").value; closeOverlay(); onPick(c); };
  $("cpOk").onclick = pick;
  $("cpHex").addEventListener("keydown", (e) => { if (e.key === "Enter") pick(); });
}

// ---------- big paste confirmation ----------
export function openPasteConfirm(lines, target, done) {
  const n = lines.length;
  const secs = Math.max(0, Math.ceil((n - 5) / 2));
  const preview = lines.slice(0, 6).map((l) => esc(l.length > 140 ? l.slice(0, 140) + "…" : l)).join("\n") + (n > 6 ? `\n… and ${n - 6} more` : "");
  open(`<div class="modal-head">Send ${n} lines to ${esc(target)}?</div>
    <div class="modal-body">
      <p style="margin:0 0 10px" class="muted">Sending many lines at once can get you kicked for flooding.${secs ? ` IRChatterbox paces them, so this takes about ${secs} second${secs === 1 ? "" : "s"}.` : ""}</p>
      <pre class="paste-preview">${preview}</pre>
    </div>
    <div class="modal-foot"><button class="btn left" id="pcCancel">Cancel</button><button class="btn" id="pcJoin">Join into one line</button><button class="btn primary" id="pcSend">Send ${n} lines</button></div>`, "sm", true);
  const finish = (mode) => { closeOverlay(); done(mode); };
  $("pcCancel").onclick = () => finish(null);
  $("pcJoin").onclick = () => finish("join");
  $("pcSend").onclick = () => finish("send");
  $("pcSend").focus();
}

// ---------- quit confirmation ----------
let quitKeys = null;
export function openQuitConfirm(connected) {
  if (quitKeys) document.removeEventListener("keydown", quitKeys, true);
  const list = connected || [];
  const what = list.length === 0 ? "You're not connected to any servers."
    : list.length === 1 ? `You'll be disconnected from <b>${esc(list[0])}</b>.`
    : `You'll be disconnected from <b>${list.slice(0, -1).map(esc).join("</b>, <b>")}</b> and <b>${esc(list[list.length - 1])}</b>.`;
  open(`<div class="modal-head">Quit IRChatterbox?</div>
    <div class="modal-body"><p style="margin:0 0 14px">${what}</p>${check("qNoAsk", false, "Don't ask me again")}</div>
    <div class="modal-foot"><button class="btn" id="qCancel">Cancel</button><button class="btn primary" id="qOk">Quit</button></div>`, "sm", true);
  const done = () => { document.removeEventListener("keydown", quitKeys, true); quitKeys = null; };
  const cancel = () => { done(); closeOverlay(); Go.CancelClose(); };
  const quit = () => { const noAsk = on("qNoAsk"); done(); Go.Quit(noAsk); };
  $("qCancel").onclick = cancel;
  $("qOk").onclick = quit;
  quitKeys = (e) => {
    if (!$("qOk")) return done();
    if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); quit(); }
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); cancel(); }
  };
  document.addEventListener("keydown", quitKeys, true);
  $("qOk").focus();
}

// ---------- server editor ----------
const PRESETS = [
  ["Libera.Chat", "irc.libera.chat"], ["OFTC", "irc.oftc.net"], ["EFnet", "irc.efnet.org"],
  ["Rizon", "irc.rizon.net"], ["Undernet", "irc.undernet.org"], ["IRCnet", "open.ircnet.net"],
];

export async function openServerEditor(cfg, onSaved) {
  const isNew = !cfg?.id;
  const c = { name: "", host: "", port: 6697, tls: true, nick: "", authMethod: "none", autoConnect: true, rememberChannels: true, autoJoin: [], perform: [], ...(cfg || {}) };
  if (!c.nick) c.nick = S.servers.find((s) => s.cfg.nick)?.cfg.nick || (await Go.GetDefaultNick()) || "";
  open(`<div class="modal-head">${isNew ? "Add server" : "Edit " + esc(c.name)}</div>
    <div class="modal-body">
      ${isNew ? `<div class="section-title">Popular networks</div>
      <div class="chips presets">${PRESETS.map(([n, h]) => `<button class="chip preset" data-host="${h}" data-name="${n}">${n}</button>`).join("")}</div>` : ""}
      <div class="section-title">Connection</div>
      <div class="fgrid">
        ${field("Name", text("sName", c.name, "Libera.Chat"))}
        ${field("Host", text("sHost", c.host, "irc.libera.chat"))}
        ${field("Port", text("sPort", c.port, "6697", "number"))}
        <div class="field"><label>&nbsp;</label>${check("sTls", c.tls, "Use TLS (SSL)")}${check("sSkip", c.tlsSkipVerify, "Accept invalid certificates")}</div>
        ${field("Server password", text("sPass", c.password, "usually empty", "password"))}
        <div class="field"><label>&nbsp;</label>${check("sAuto", c.autoConnect, "Connect automatically on startup")}</div>
        ${field("Text encoding", `<select id="sEnc"></select>`, "Older networks may use a regional encoding. Automatic suits almost every server.", true)}
      </div>
      <div class="section-title">Identity</div>
      <div class="fgrid">
        ${field("Nickname", text("sNick", c.nick, "Andrew"))}
        ${field("Alternate nickname", text("sAlt", c.altNick, "Andrew_"))}
        ${field("Username (ident)", text("sUser", c.username, "same as nick"))}
        ${field("Real name", text("sReal", c.realname, "IRChatterbox user"))}
      </div>
      <div class="section-title">Automatic login</div>
      <div class="fgrid">
        ${field("Method", `<select id="sAuthM">
            <option value="none" ${c.authMethod === "none" ? "selected" : ""}>None</option>
            <option value="sasl" ${c.authMethod === "sasl" ? "selected" : ""}>SASL PLAIN (recommended)</option>
            <option value="nickserv" ${c.authMethod === "nickserv" ? "selected" : ""}>NickServ IDENTIFY</option></select>`)}
        <div></div>
        ${field("Account name", text("sAuthU", c.authUser, "defaults to nickname"))}
        ${field("Password", text("sAuthP", c.authPass, "", "password"))}
      </div>
      <div class="section-title">Channels & commands</div>
      <div class="fgrid">
        ${field("Auto-join channels", `<textarea id="sJoin" placeholder="#linux&#10;#secret key">${esc((c.autoJoin || []).join("\n"))}</textarea>`, "One per line. Add a key after a space for keyed channels.")}
        ${field("Perform on connect", `<textarea id="sPerform" placeholder="/mode $me +x&#10;/msg someone hello">${esc((c.perform || []).join("\n"))}</textarea>`, "Commands run after connecting. $me = your nick.")}
        <div class="full">${check("sRemember", c.rememberChannels, "Remember channels I join and leave (keeps the auto-join list up to date)")}</div>
      </div>
    </div>
    <div class="modal-foot">
      ${isNew ? "" : `<button class="btn danger left" id="sDel">Remove</button>`}
      <span class="form-err ${isNew ? "left" : ""}" id="sErr"></span>
      <button class="btn" id="sCancel">Cancel</button>
      <button class="btn" id="sSave">Save</button>
      <button class="btn primary" id="sSaveC">Save & connect</button>
    </div>`, "", true);
  document.querySelectorAll(".preset").forEach((b) => (b.onclick = () => {
    $("sHost").value = b.dataset.host; $("sName").value = b.dataset.name;
    $("sHost").classList.remove("invalid"); $("sErr").textContent = "";
    $("sTls").checked = true; $("sPort").value = 6697;
    document.querySelectorAll(".preset").forEach((x) => x.classList.toggle("on", x === b));
    $("sNick").focus();
  }));
  Go.GetEncodings().then((list) => {
    $("sEnc").innerHTML = (list || [["auto", "Automatic"]]).map(([v, l]) => `<option value="${v}" ${(c.encoding || "auto") === v ? "selected" : ""}>${esc(l)}</option>`).join("");
  });
  const port = () => {
    if (!$("sPort").dataset.touched) $("sPort").value = on("sTls") ? 6697 : 6667;
  };
  $("sPort").oninput = () => ($("sPort").dataset.touched = 1);
  $("sTls").onchange = port;
  $("sHost").oninput = () => { if (!$("sName").dataset.touched) $("sName").value = prettyName(val("sHost")); };
  $("sName").oninput = () => ($("sName").dataset.touched = 1);
  if (!isNew) $("sName").dataset.touched = 1;
  $("sCancel").onclick = closeOverlay;
  const save = async (connect) => {
    const missing = !val("sHost") ? "sHost" : !val("sNick") ? "sNick" : null;
    document.querySelectorAll(".field input.invalid").forEach((x) => x.classList.remove("invalid"));
    if (missing) {
      $(missing).classList.add("invalid");
      $(missing).focus();
      $("sErr").textContent = missing === "sHost" ? "Enter a server host (or pick a network above)" : "Enter a nickname";
      return;
    }
    const out = {
      ...c, name: val("sName") || val("sHost"), host: val("sHost"), port: parseInt(val("sPort")) || 6697, tls: on("sTls"), tlsSkipVerify: on("sSkip"),
      password: $("sPass").value, nick: val("sNick"), altNick: val("sAlt"), username: val("sUser"), realname: val("sReal"),
      authMethod: $("sAuthM").value, authUser: val("sAuthU"), authPass: $("sAuthP").value, autoConnect: on("sAuto"),
      rememberChannels: on("sRemember"), autoJoin: lines("sJoin"), perform: lines("sPerform"), encoding: $("sEnc").value,
    };
    const saved = await Go.SaveServer(out);
    if (!saved?.id) { $("sErr").textContent = "Couldn't save the server"; return; }
    closeOverlay();
    onSaved(saved, connect);
  };
  document.querySelector(".modal").addEventListener("input", (e) => {
    if (e.target.classList.contains("invalid") && e.target.value.trim()) { e.target.classList.remove("invalid"); $("sErr").textContent = ""; }
  });
  // Enter in any single-line field = Save & connect
  document.querySelector(".modal").addEventListener("keydown", (e) => {
    if (e.key === "Enter" && e.target.tagName === "INPUT") { e.preventDefault(); save(true); }
  });
  $("sSave").onclick = () => save(false);
  $("sSaveC").onclick = () => save(true);
  if ($("sDel")) $("sDel").onclick = () => openPrompt(`Remove ${c.name}?`, "", null, async () => { await Go.DeleteServer(c.id); location.reload(); }, "Remove");
  (isNew ? $("sHost") : $("sNick")).focus();
}

function prettyName(host) {
  const known = { "irc.libera.chat": "Libera.Chat", "irc.oftc.net": "OFTC", "irc.efnet.org": "EFnet", "irc.rizon.net": "Rizon", "irc.undernet.org": "Undernet" };
  if (known[host]) return known[host];
  const parts = host.split(".");
  return parts.length > 2 ? parts[parts.length - 2][0].toUpperCase() + parts[parts.length - 2].slice(1) : host;
}

// ---------- settings ----------
let tab = "general";
export function openSettings(startTab) {
  if (startTab) tab = startTab;
  const set = structuredClone(S.cfg.settings);
  const plug = structuredClone(S.cfg.plugins);
  const shortcuts = structuredClone(S.cfg.shortcuts || []);
  plug.urlCommands ||= [];
  plug.music ||= { enabled: true, trigger: "!music", source: "auto", cooldown: 10, format: "Now playing: {np}", npFormat: "is listening to {np}" };
  const m = open(`<div class="modal-head"><div class="grow">Settings<div class="modal-sub">Customize your IRC experience.</div></div><button class="icon-btn" id="stX">✕</button></div>
    <div class="tabs">${["general", "appearance", "highlights", "shortcuts", "media", "plugins", "about"].map((t) => `<button class="tab ${t === tab ? "on" : ""}" data-t="${t}">${t === "highlights" ? "Notifications" : t[0].toUpperCase() + t.slice(1)}</button>`).join("")}</div>
    <div class="modal-body" id="stBody"></div>
    <div class="modal-foot"><button class="btn left" id="stFolder">Open data folder</button><button class="btn" id="stCancel">Cancel</button><button class="btn primary" id="stSave">Save changes</button></div>`, "settings");
  m.querySelector(".tabs").onclick = (e) => {
    const t = e.target.closest("[data-t]");
    if (!t) return;
    collect();
    tab = t.dataset.t;
    m.querySelectorAll(".tab").forEach((x) => x.classList.toggle("on", x === t));
    draw();
  };
  $("stX").onclick = $("stCancel").onclick = () => { applySettings(); closeOverlay(); };
  $("stFolder").onclick = () => Go.OpenDataFolder();
  $("stSave").onclick = async () => {
    collect();
    const err = await Go.SaveSettings(set);
    await Go.SavePlugins(plug);
    await Go.SaveShortcuts(shortcuts.filter((x) => x.trigger.trim() && x.text));
    await reloadConfig();
    closeOverlay();
    toast(err || "Settings saved", err ? "error" : "ok");
  };

  const body = $("stBody");
  const soundPicker = (id, value) => `<div style="display:flex;gap:8px">
        <select id="${id}" style="flex:1">
          <option value="">None</option>
          ${BUILTIN_SOUNDS.map(([v, l]) => `<option value="${v}" ${value === v ? "selected" : ""}>${l}</option>`).join("")}
          <option value="file" ${(value || "").startsWith("file:") ? "selected" : ""}>Custom sound file…</option>
        </select>
        <button class="btn sm" data-test-sound="${id}">▶ Test</button></div>
        <div class="snd-file ${(value || "").startsWith("file:") ? "" : "hidden"}" id="${id}File" style="display:flex;gap:8px;margin-top:6px">
          <input type="text" id="${id}Path" value="${esc((value || "").startsWith("file:") ? value.slice(5) : "")}" placeholder="C:\\Sounds\\ping.wav" style="flex:1">
          <button class="btn sm" data-browse-sound="${id}">Browse…</button></div>`;
  const readSound = (id) => {
    const v = $(id).value;
    return v === "file" ? (val(id + "Path") ? "file:" + val(id + "Path") : "") : v;
  };
  function draw() {
    if (tab === "general") body.innerHTML =
      card("Chat display", "Control how chats look and what information is shown.", `
      <div class="fgrid">
        ${field("Time format", `<select id="gTime"><option value="24h">24-hour</option><option value="12h" ${set.timeFormat === "12h" ? "selected" : ""}>12-hour</option></select>`)}
        <div class="field"><label>&nbsp;</label>${check("gSec", set.showSeconds, "Show seconds")}</div>
        ${field("Font size", text("gFs", set.fontSize, "14", "number"))}
        ${field("Chat font", text("gFont", set.fontFamily, "Segoe UI (default) — e.g. Cascadia Mono, Consolas"))}
      </div>
      <div class="set-sep"></div>
      ${check("gJoins", set.showJoinsParts, "Show joins, parts and quits")}
      ${check("gMarker", set.markerLine !== false, "Mark unread messages with a divider line")}
      ${check("gCompact", set.compactMode, "Compact line spacing")}
      ${check("gUsers", S.ui.users !== false, "Show the user list")}`) +
      card("History & logging", "Manage how much chat history is kept and whether to save it to files.", `
      ${check("gLog", set.logging, "Keep chat history between restarts")}
      <div class="fgrid" style="margin-top:10px">
        ${field("History shown when opening a channel", text("gHist", set.historyLines, "200", "number"))}
        ${field("Maximum saved lines per channel", text("gMax", set.logMaxLines ?? 10000, "10000", "number"), "Older lines are removed from the history and .txt files. 0 = keep everything.")}
      </div>
      <div class="set-sep"></div>
      ${check("gTxt", set.textLogs, "Save each channel to a text log file (.txt)")}
      <div class="hint indent">Readable logs like <code>[2026-10-16 09:41:22] &lt;@Andrew&gt; hello</code> — one file per channel and private chat.</div>
      <div class="field" id="gTxtRow" style="margin-top:10px"><label>Log folder</label>
        <div style="display:flex;gap:8px"><input type="text" id="gFolder" value="${esc(set.logFolder || "")}" placeholder="Default: the IRChatterbox data folder\\logs" style="flex:1">
        <button class="btn sm" id="gBrowse">Browse…</button><button class="btn sm" id="gOpenLogs">Open</button></div></div>`) +
      card("Startup & closing", "Configure how IRChatterbox starts and exits.", `
      ${check("gAuto", set.startWithWindows, "Launch when I sign in to Windows")}
      <div class="indent">${check("gMin", set.startMinimized, "Start minimised to the taskbar")}</div>
      ${check("gConfirm", set.confirmOnClose, "Confirm before quitting when I close the window")}
      ${check("gTray", set.trayIcon !== false, "Show an icon in the system tray")}
      <div class="indent">${check("gCloseTray", set.closeToTray, "Closing the window hides it to the tray (quit from the tray menu)")}</div>`) +
      card("Away", "Mark yourself away on every network when you step away from your PC.", `
      ${check("gAway", set.autoAway, "Set me away automatically when I'm idle")}
      <div class="fgrid" style="margin-top:10px">
        ${field("After this many minutes without keyboard or mouse input", text("gAwayMin", set.autoAwayMinutes ?? 15, "15", "number"))}
        ${field("Away message", text("gAwayMsg", set.autoAwayMessage ?? "Away from keyboard", "Away from keyboard"))}
      </div>
      <div class="hint">You're set back as soon as you use your PC again. A manual <code>/away</code> is never undone automatically.</div>`) +
      card("Sending", "Protection against flooding a channel by accident.", `
      <div class="fgrid">${field("Ask before sending a paste of this many lines or more", text("gPaste", set.pasteWarnLines ?? 5, "5", "number"), "0 = never ask. You can send it as-is, join it into one line, or cancel.")}</div>`) +
      card("Messages", "The default message sent when you quit or leave a channel.", `
      <div class="fgrid">${field("Quit / part message", text("gQuit", set.quitMessage, "IRChatterbox"), "", true)}</div>`);
    else if (tab === "appearance") drawAppearance();
    else if (tab === "highlights") body.innerHTML =
      card("Highlight words", "Lines containing your nick or these words are highlighted, badged in red and trigger a notification.", `
      ${field("Words", `<textarea id="hWords" placeholder="andrew&#10;tshirtgang">${esc((set.highlightWords || []).join("\n"))}</textarea>`, "One per line. Your current nick is always a highlight.")}`) +
      card("Windows notifications", "Pop-up alerts when someone needs your attention.", `
      ${check("hNotify", set.notifications, "Notify me when I'm highlighted and not looking at that channel")}
      ${check("hPm", set.notifyPMs, "Also notify for private messages")}
      <div style="margin-top:10px"><button class="btn sm" id="hTest">Send a test notification</button></div>`) +
      card("Sounds", "Play a sound when you're mentioned or messaged while looking elsewhere.", `
      <div class="fgrid">
        ${field("When I'm highlighted", soundPicker("sndHl", set.soundHighlight ?? "chime"))}
        ${field("For private messages", soundPicker("sndPm", set.soundPM ?? "ping"))}
        ${field("Volume", `<div style="display:flex;gap:10px;align-items:center"><input type="range" id="sndVol" min="0" max="100" value="${set.soundVolume ?? 70}" style="flex:1"><span id="sndVolOut" class="muted" style="width:36px">${set.soundVolume ?? 70}%</span></div>`)}
      </div>
      <div class="hint">Custom sounds can be .wav, .mp3, .ogg or .m4a. When a sound is set, the Windows notification itself stays silent so you don't hear two.</div>`) +
      card("Tray & taskbar", "Get your attention when you're in another app.", `
      ${check("hFlash", set.flashOnPing !== false, "Flash the tray icon and taskbar button when I'm mentioned or messaged")}
      <div class="hint indent">Flashing stops as soon as you switch back to IRChatterbox. Tray icon options are in General → Startup & closing.</div>`) +
      card("Ignore list", "Hide messages from these people entirely.", `
      ${field("Ignored nicks or masks", `<textarea id="hIgnore" placeholder="spammer&#10;*!*@bad.host">${esc((set.ignore || []).join("\n"))}</textarea>`, "Nick, or a nick!user@host mask with * wildcards.")}`);
    else if (tab === "shortcuts") drawShortcuts();
    else if (tab === "media") body.innerHTML =
      card("Inline media", "Show pictures and videos right in the chat.", `
      ${check("mImg", set.inlineImages, "Show images inline (PNG, JPG, GIF, WEBP, AVIF and imgur links)")}
      ${check("mYt", set.inlineYouTube, "Show YouTube previews — click to play inside the chat")}
      <div class="hint" style="margin-top:10px">Previews load directly from the linked site, which means that site sees your IP address. Turn these off if that matters to you.</div>`);
    else if (tab === "plugins") drawPlugins();
    else if (tab === "about") drawAbout();
    wire();
  }

  function wire() {
    if ($("gTxt")) {
      const sync = () => { $("gTxtRow").style.opacity = on("gTxt") ? 1 : 0.5; };
      $("gTxt").addEventListener("change", sync); sync();
      $("gBrowse").onclick = async () => { const f = await Go.PickFolder("Choose where to save text logs"); if (f) $("gFolder").value = f; };
      $("gOpenLogs").onclick = () => Go.OpenLogFolder();
    }
    if ($("hTest")) $("hTest").onclick = () => { collect(); Go.SaveSettings(set); Go.Notify("IRChatterbox", "This is what a highlight notification looks like."); };
    if ($("sndVol")) {
      $("sndVol").oninput = () => ($("sndVolOut").textContent = $("sndVol").value + "%");
      for (const id of ["sndHl", "sndPm"]) {
        $(id).onchange = () => $(id + "File").classList.toggle("hidden", $(id).value !== "file");
      }
      body.querySelectorAll("[data-test-sound]").forEach((b) => (b.onclick = () => previewSound(readSound(b.dataset.testSound), +$("sndVol").value)));
      body.querySelectorAll("[data-browse-sound]").forEach((b) => (b.onclick = async () => {
        const f = await Go.PickSoundFile();
        if (f) $(b.dataset.browseSound + "Path").value = f;
      }));
    }
  }

  function drawAppearance(selectTheme) {
    const themes = allThemes();
    const opts = (sel) => Object.keys(themes).map((n) => `<option ${n === sel ? "selected" : ""}>${esc(n)}</option>`).join("");
    body.innerHTML = card("Theme", "Pick a theme, or follow Windows light and dark mode automatically.", `
      <div class="fgrid">
        ${field("Mode", `<select id="aMode">
          <option value="auto" ${set.theme === "auto" ? "selected" : ""}>Automatic — follow Windows light/dark</option>
          ${Object.keys(themes).map((n) => `<option value="${esc(n)}" ${set.theme === n ? "selected" : ""}>Always ${esc(n)}</option>`).join("")}</select>`, "", true)}
        ${field("Light theme (used in auto mode)", `<select id="aLight">${opts(set.lightTheme)}</select>`)}
        ${field("Dark theme (used in auto mode)", `<select id="aDark">${opts(set.darkTheme)}</select>`)}
      </div>`) + card("Theme editor", "Start from any theme, change its colours and save it as your own. Edits preview live.", `
      <div class="fgrid">
        ${field("Start from", `<select id="tBase">${opts(selectTheme || resolveTheme(set).name)}</select>`)}
        ${field("New theme name", text("tName", "", "My theme"))}
      </div>
      <div class="theme-vars" id="tVars" style="margin-top:12px"></div>
      <div style="display:flex;gap:8px;margin-top:12px">
        <button class="btn primary sm" id="tSave">Save theme</button>
        <button class="btn danger sm" id="tDel">Delete selected custom theme</button>
        <span id="tStatus" class="t-status"></span>
      </div>
      <div class="hint" style="margin-top:10px">Themes are saved as JSON in the themes folder — share them by copying the file.</div>`) + card("Nick colours", "Custom colours you've set. They apply to every variant of a nick (Andrew, Andrew|away…).", `
      ${check("aContrast", set.nickContrast !== false, "Keep custom colours readable in light and dark mode")}
      <div class="hint indent" style="margin-bottom:12px">If a colour is too close to the background — like black in dark mode — its lightness is flipped (black → white, navy → light blue). Your chosen colour is kept and used whenever it's readable.</div>
      <div class="chips" id="ncList">${Object.entries(S.cfg.nickColors || {}).map(([n, c]) => `<span class="chip"><span class="swatch" style="background:${c}"></span><span style="color:${nickColor(n)}">${esc(n)}</span><button data-nc="${esc(n)}">✕</button></span>`).join("") || `<span class="muted">No custom nick colours yet. Use <code>/color nick #hex</code> or right-click a nick.</span>`}</div>`);
    const drawVars = () => {
      const t = getTheme($("tBase").value) || BUILTIN.Dark;
      if (!$("tName").value || $("tName").dataset.auto) {
        $("tName").value = t.builtin ? t.name + " custom" : t.name;
        $("tName").dataset.auto = 1;
      }
      $("tVars").innerHTML = `<div class="tv"><span>Base</span><select id="tB"><option value="dark">Dark</option><option value="light" ${t.base === "light" ? "selected" : ""}>Light</option></select></div><div></div>` +
        THEME_VARS.map(([k, label]) => {
          const v = t.vars[k] || "#000000";
          return `<div class="tv"><input type="color" data-k="${k}" value="${v.slice(0, 7)}"><input type="text" data-kt="${k}" value="${esc(v)}"><span>${label}</span></div>`;
        }).join("");
      preview();
    };
    const current = () => {
      const vars = {};
      $("tVars").querySelectorAll("[data-kt]").forEach((i) => (vars[i.dataset.kt] = i.value));
      return { name: val("tName"), base: $("tB").value, vars };
    };
    const preview = () => applyTheme(current());
    $("tBase").onchange = () => { $("tName").dataset.auto = 1; $("tName").value = ""; drawVars(); };
    $("tName").oninput = () => delete $("tName").dataset.auto;
    $("tVars").addEventListener("input", (e) => {
      const k = e.target.dataset.k || e.target.dataset.kt;
      if (!k) return;
      const color = $("tVars").querySelector(`[data-k="${k}"]`), txt = $("tVars").querySelector(`[data-kt="${k}"]`);
      if (e.target === color) txt.value = color.value; else if (/^#[0-9a-f]{6}$/i.test(txt.value)) color.value = txt.value;
      preview();
    });
    $("tVars").addEventListener("change", preview);
    const status = (msg, ok = true) => {
      const el = $("tStatus");
      el.textContent = msg;
      el.className = "t-status " + (ok ? "ok" : "bad");
    };
    $("tSave").onclick = async () => {
      const t = current();
      if (!t.name) { status("Give the theme a name first", false); $("tName").focus(); return; }
      if (BUILTIN[t.name]) { status(`“${t.name}” is a built-in theme — pick another name`, false); $("tName").focus(); return; }
      const err = await Go.SaveTheme(t);
      if (err) { status("Couldn't save: " + err, false); return; }
      setCustomThemes(await Go.GetThemes());
      collect();
      // use it straight away: in auto mode fill the matching light/dark slot, otherwise switch to it
      let where;
      if (set.theme === "auto") {
        if (t.base === "light") set.lightTheme = t.name; else set.darkTheme = t.name;
        where = `now your ${t.base} theme in automatic mode`;
      } else {
        set.theme = t.name;
        where = "now your active theme";
      }
      await Go.SaveSettings(set);
      S.cfg.settings = structuredClone(set);
      applySettings();
      const msg = `✓ Saved “${t.name}” — ${where}`;
      drawAppearance(t.name);
      status(msg);
      toast(`Theme “${t.name}” saved`, "ok");
    };
    $("tDel").onclick = async () => {
      const n = $("tBase").value;
      if (BUILTIN[n]) { status("Built-in themes can't be deleted", false); return; }
      await Go.DeleteTheme(n);
      setCustomThemes(await Go.GetThemes());
      for (const k of ["theme", "lightTheme", "darkTheme"]) if (set[k] === n) set[k] = k === "theme" ? "auto" : k === "lightTheme" ? "Light" : "Dark";
      await Go.SaveSettings(set);
      S.cfg.settings = structuredClone(set);
      applySettings();
      drawAppearance();
      toast(`Theme “${n}” deleted`, "ok");
    };
    $("ncList").onclick = async (e) => {
      const b = e.target.closest("[data-nc]");
      if (!b) return;
      await Go.SetNickColor(b.dataset.nc, "");
      delete S.cfg.nickColors[b.dataset.nc];
      b.closest(".chip").remove();
    };
    for (const id of ["aMode", "aLight", "aDark"]) $(id).onchange = () => { collect(); applyTheme(resolveTheme(set)); };
    drawVars();
  }

  async function drawAbout() {
    body.innerHTML = `
      <section class="set-card about">
        <img class="about-logo for-light" src="${wordmarkLight}" alt="IRChatterbox">
        <img class="about-logo for-dark" src="${wordmarkDark}" alt="IRChatterbox">
        <p class="about-tag">A modern IRC client for Windows.</p>
        <div class="about-ver">Version <b id="abVer">…</b></div>
        <p class="about-by">Created by <b>Andrew Lyle</b> · <a href="https://github.com/phpandrew" data-url="https://github.com/phpandrew">@phpandrew</a></p>
        <div class="about-actions">
          <button class="btn primary gh" data-open="${REPO}"><img src="${githubIcon}" alt="">View on GitHub</button>
          <button class="btn" data-open="${REPO}/releases">Releases</button>
          <button class="btn" data-open="${REPO}/issues">Report an issue</button>
        </div>
        <a class="about-url" href="${REPO}" data-url="${REPO}">${REPO.replace("https://", "")}</a>
      </section>
      ${card("Updates", "New versions are published on GitHub Releases.", `
        <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
          <button class="btn sm" id="abCheck">Check for updates</button>
          <span id="abStatus" class="muted" style="font-size:12.5px"></span>
        </div>
        <div style="margin-top:10px">${check("abAuto", set.autoUpdateCheck !== false, "Check for updates automatically")}</div>`)}
      <p class="about-foot">Built with Go and Wails · Emoji data from unicode-emoji-json (MIT)</p>`;
    body.querySelectorAll("[data-open]").forEach((b) => (b.onclick = () => Go.OpenURL(b.dataset.open)));
    $("abVer").textContent = (await Go.GetVersion()) || "dev";
    $("abCheck").onclick = async () => {
      const st = $("abStatus");
      st.className = "muted"; st.textContent = "Checking…";
      const info = await Go.CheckForUpdate();
      if (info.error) { st.className = "t-status bad"; st.textContent = info.error; return; }
      if (!info.available) { st.className = "t-status ok"; st.textContent = `✓ You're on the latest version (${info.current}).`; return; }
      st.className = "t-status ok";
      st.innerHTML = `Version ${esc(info.latest)} is available. <button class="btn sm primary" id="abInstall">Update &amp; restart</button>`;
      $("abInstall").onclick = () => { closeOverlay(); showUpdateBar(info); document.getElementById("ubInstall")?.click(); };
    };
  }

  function drawShortcuts() {
    body.innerHTML = card("Text shortcuts", "Type a shortcut and it's replaced with its text.", `
      <p class="hint" style="margin:0 0 12px">Type a shortcut in any message and it's replaced with its text, e.g. <code>$whatever</code> → <code>¯\\_(ツ)_/¯</code>. It expands when you press space or send, and typing <code>$</code> shows the list. The text can also be a command like <code>/me waves</code>. You can also manage shortcuts with <code>/shortcut add $name text</code>.</p>
      <div class="sc-row muted" style="font-size:11.5px;font-weight:700"><span>SHORTCUT</span><span>REPLACED WITH</span><span></span></div>
      <div id="scList"></div>
      <button class="btn sm" id="scAdd">+ Add shortcut</button>`);
    const draw = () => {
      $("scList").innerHTML = shortcuts.map((sc, i) => `<div class="sc-row" data-i="${i}">
        <input class="trig" data-f="trigger" value="${esc(sc.trigger)}" placeholder="$name">
        <input data-f="text" value="${esc(sc.text)}" placeholder="replacement text">
        <button class="icon-btn" data-rm="${i}" title="Remove">✕</button></div>`).join("");
    };
    draw();
    $("scList").addEventListener("input", (e) => {
      const row = e.target.closest("[data-i]");
      if (!row) return;
      let v = e.target.value;
      if (e.target.dataset.f === "trigger") v = v.replace(/\s/g, "");
      shortcuts[+row.dataset.i][e.target.dataset.f] = v;
    });
    $("scList").addEventListener("click", (e) => { if (e.target.dataset.rm) { shortcuts.splice(+e.target.dataset.rm, 1); draw(); } });
    $("scAdd").onclick = () => { shortcuts.push({ trigger: "$", text: "" }); draw(); const ins = $("scList").querySelectorAll(".trig"); ins[ins.length - 1].focus(); };
  }

  function drawPlugins() {
    body.innerHTML = card("Bot replies", "Applies to every automatic reply below.", `
      <div class="fgrid">
        ${field("Bot reply prefix", text("pPrefix", plug.botPrefix, "[Bot] "), "Put in front of every automatic reply. Messages starting with it are never answered (prevents bot loops).")}
      </div>
      </div>`) + card("Temperature converter", "Replies with a °C ⇄ °F conversion when a message mentions a temperature.", `
      ${check("pW", plug.weatherEnabled, "Enabled")}
      <div class="indent">
        ${check("pWMe", plug.weatherFromMe, "For my own messages")}
        ${check("pWOthers", plug.weatherFromOthers, "For other people's messages")}
      </div>
      <div class="fgrid" style="margin-top:8px">
        ${field("Only in these channels", text("pWChans", (plug.weatherChannels || []).join(" "), "all channels"), "Space separated, e.g. #neowin #lounge. Empty = everywhere.")}
        ${field("Cooldown per channel (seconds)", text("pWCd", plug.weatherCooldown, "5", "number"))}
      </div>
      <div class="hint">Example: “Today it feels like 20c” → <code>${esc(plug.botPrefix)}20 C = 68 F</code></div>`) +
      card("Now playing (MusicBee)", "Replies with your current song when someone types the trigger. /np shares it as an action.", `
      ${check("muOn", plug.music.enabled, "Enabled")}
      <div class="indent">${check("muMe", plug.music.onlyMe, "Only I can trigger it")}</div>
      <div class="fgrid" style="margin-top:8px">
        ${field("Trigger", text("muTrig", plug.music.trigger, "!music"))}
        ${field("Where to read the song from", `<select id="muSrc">
          ${[["auto", "Automatic (try each below)"], ["smtc", "Windows media controls"], ["window", "MusicBee window title"], ["file", "Text file (Now Playing plugin)"]]
            .map(([v, l]) => `<option value="${v}" ${plug.music.source === v ? "selected" : ""}>${l}</option>`).join("")}</select>`)}
        <div class="field full"><label>Text file</label><div style="display:flex;gap:8px"><input type="text" id="muFile" value="${esc(plug.music.file || "")}" placeholder="e.g. C:\\Users\\Andrew\\Documents\\nowplaying.txt" style="flex:1"><button class="btn sm" id="muBrowse">Browse…</button></div>
          <div class="hint">Optional. Written by MusicBee's “Now Playing to External Files” plugin.</div></div>
        ${field("Reply", text("muFmt", plug.music.format, "Now playing: {np}"), "{np} = Artist - Title · also {artist} {title} {album} {player}")}
        ${field("/np action", text("muNp", plug.music.npFormat, "is listening to {np}"), "Typing /np sends “* you is listening to …”")}
        ${field("Cooldown per channel (seconds)", text("muCd", plug.music.cooldown, "10", "number"))}
        <div class="field" style="align-self:end"><button class="btn sm" id="muTest">Test now</button></div>
      </div>
      <div class="test-out hidden" id="muOut"></div>`) +
      card("URL commands", "Typing the trigger fetches a web address and posts the result — like !solar.", `
      <p class="hint" style="margin:0 0 12px">Typing the trigger fetches the URL and posts the result. Use <code>{args}</code> in the URL for text after the trigger. JSON responses can be shaped with a template like <code>Solar: {{.power}} W, today {{round .today_kwh 1}} kWh</code>.</p>
      <div id="ucList"></div>
      <button class="btn sm" id="ucAdd">+ Add command</button>`) +
      card("Script plugins", "Your own JavaScript plugins from the plugins folder.", `
      ${check("pScripts", plug.scriptsEnabled, "Run JavaScript plugins from the plugins folder")}
      <div style="display:flex;gap:8px;margin-top:8px"><button class="btn sm" id="pOpen">Open data folder</button><button class="btn sm" id="pReload">Reload plugins</button><span class="muted" id="pReloadOut" style="align-self:center;font-size:12px"></span></div>
      <div class="hint" style="margin-top:10px">See <code>plugins/_example.js</code> for the API (on, reply, say, httpGet…).</div>`);
    const drawUc = () => {
      $("ucList").innerHTML = plug.urlCommands.map((u, i) => `
        <div class="urlcmd" data-i="${i}">
          <div class="row">
            ${field("Trigger", `<input type="text" data-f="trigger" value="${esc(u.trigger)}" placeholder="!solar">`)}
            <div class="field" style="flex:3"><label>URL</label><input type="text" data-f="url" value="${esc(u.url)}" placeholder="https://example.com/solar.json"></div>
          </div>
          ${field("Template (optional, for JSON)", `<input type="text" data-f="template" value="${esc(u.template)}" placeholder="Solar now: {{.power}} W">`)}
          <div class="row" style="margin-top:8px">
            ${field("Max lines", `<input type="number" data-f="maxLines" value="${u.maxLines || 3}">`)}
            ${field("Cooldown (s)", `<input type="number" data-f="cooldown" value="${u.cooldown ?? 30}">`)}
            <div class="field" style="flex:2"><label class="check"><input type="checkbox" data-f="enabled" ${u.enabled ? "checked" : ""}> Enabled</label><label class="check"><input type="checkbox" data-f="onlyMe" ${u.onlyMe ? "checked" : ""}> Only I can trigger it</label></div>
            <div style="display:flex;gap:6px;padding-bottom:4px"><button class="btn sm" data-test="${i}">Test</button><button class="btn sm danger" data-rm="${i}">Remove</button></div>
          </div>
          <div class="test-out hidden" id="ucOut${i}"></div>
        </div>`).join("");
    };
    drawUc();
    $("ucList").addEventListener("input", (e) => {
      const card = e.target.closest("[data-i]"), f = e.target.dataset.f;
      if (!card || !f) return;
      const u = plug.urlCommands[+card.dataset.i];
      u[f] = e.target.type === "checkbox" ? e.target.checked : e.target.type === "number" ? parseInt(e.target.value) || 0 : e.target.value;
    });
    $("ucList").addEventListener("click", async (e) => {
      if (e.target.dataset.rm) { plug.urlCommands.splice(+e.target.dataset.rm, 1); drawUc(); }
      if (e.target.dataset.test) {
        const i = +e.target.dataset.test, out = $("ucOut" + i);
        out.classList.remove("hidden");
        out.textContent = "Fetching…";
        const u = { ...plug.urlCommands[i], url: plug.urlCommands[i].url.replace("{args}", "") };
        out.textContent = (await Go.TestURLCommand(u)).split("\n").map((l) => (plug.botPrefix || "") + l).join("\n");
      }
    });
    $("ucAdd").onclick = () => { plug.urlCommands.push({ trigger: "!", url: "", template: "", maxLines: 3, cooldown: 30, enabled: true, onlyMe: true }); drawUc(); };
    const readMusic = () => ({ ...plug.music, enabled: on("muOn"), onlyMe: on("muMe"), trigger: val("muTrig") || "!music", source: $("muSrc").value,
      file: val("muFile"), format: $("muFmt").value, npFormat: $("muNp").value, cooldown: parseInt(val("muCd")) || 0 });
    $("muBrowse").onclick = async () => { const f = await Go.PickFile("Choose the now-playing text file"); if (f) $("muFile").value = f; };
    $("muTest").onclick = async () => {
      const out = $("muOut");
      out.classList.remove("hidden");
      out.textContent = "Checking…";
      out.textContent = ((await Go.TestMusic(readMusic())) || []).join("\n");
    };
    $("pOpen").onclick = () => Go.OpenDataFolder();
    $("pReload").onclick = async () => ($("pReloadOut").textContent = await Go.ReloadPlugins());
  }

  function collect() {
    const has = (id) => !!$(id);
    if (has("gTime")) {
      set.timeFormat = $("gTime").value; set.showSeconds = on("gSec"); set.fontSize = parseInt(val("gFs")) || 14;
      set.fontFamily = val("gFont"); set.showJoinsParts = on("gJoins"); set.compactMode = on("gCompact"); set.markerLine = on("gMarker");
      set.logging = on("gLog"); set.historyLines = parseInt(val("gHist")) || 0;
      set.textLogs = on("gTxt"); set.logFolder = val("gFolder"); set.logMaxLines = Math.max(0, parseInt(val("gMax")) || 0); set.quitMessage = val("gQuit");
      set.startWithWindows = on("gAuto"); set.startMinimized = on("gMin"); set.confirmOnClose = on("gConfirm");
      set.trayIcon = on("gTray"); set.closeToTray = on("gCloseTray");
      set.autoAway = on("gAway"); set.autoAwayMinutes = Math.max(1, parseInt(val("gAwayMin")) || 15); set.autoAwayMessage = val("gAwayMsg");
      set.pasteWarnLines = Math.max(0, parseInt(val("gPaste")) || 0);
      S.ui.users = on("gUsers");
      try { localStorage.setItem("ui", JSON.stringify(S.ui)); } catch {}
    }
    if (has("aMode")) { set.theme = $("aMode").value; set.lightTheme = $("aLight").value; set.darkTheme = $("aDark").value; set.nickContrast = on("aContrast"); }
    if (has("hWords")) {
      set.highlightWords = lines("hWords"); set.notifications = on("hNotify"); set.notifyPMs = on("hPm"); set.ignore = lines("hIgnore");
      set.soundHighlight = readSound("sndHl"); set.soundPM = readSound("sndPm"); set.soundVolume = +$("sndVol").value;
      set.flashOnPing = on("hFlash");
    }
    if (has("abAuto")) set.autoUpdateCheck = on("abAuto");
    if (has("mImg")) { set.inlineImages = on("mImg"); set.inlineYouTube = on("mYt"); }
    if (has("pPrefix")) {
      plug.botPrefix = $("pPrefix").value; plug.weatherEnabled = on("pW"); plug.weatherFromMe = on("pWMe"); plug.weatherFromOthers = on("pWOthers");
      plug.weatherChannels = val("pWChans").split(/[\s,]+/).filter(Boolean); plug.weatherCooldown = parseInt(val("pWCd")) || 0;
      plug.scriptsEnabled = on("pScripts");
      plug.music = { ...plug.music, enabled: on("muOn"), onlyMe: on("muMe"), trigger: val("muTrig") || "!music", source: $("muSrc").value,
        file: val("muFile"), format: $("muFmt").value, npFormat: $("muNp").value, cooldown: parseInt(val("muCd")) || 0 };
    }
  }
  draw();
}
