import { Go, on as onEvent } from "./api.js";
import wordmarkLight from "./assets/wordmark-light.png";
import wordmarkDark from "./assets/wordmark-dark.png";
import githubIcon from "./assets/github.png";
import { BUILTIN_SOUNDS, previewSound } from "./sounds.js";
import { showUpdateBar } from "./main.js";
import { android } from "./mobile.js";
import { os } from "./platform.js";

const REPO = "https://github.com/phpandrew/IRChatterbox";
import { S, closeOverlay, applySettings, reloadConfig, toast } from "./main.js";
import { esc, nickColor, iconLetters } from "./format.js";
import { LANGUAGES, setLanguage, t } from "./i18n.js";
import { THEME_VARS, BUILTIN, allThemes, applyTheme, getTheme, resolveTheme, setCustomThemes, themeOptions, onSystemThemeChange } from "./themes.js";

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
const card = (title, desc, inner, cls = "") =>
  `<section class="set-card ${cls}"><div class="set-head"><h3>${title}</h3>${desc ? `<p>${desc}</p>` : ""}</div><div class="set-body">${inner}</div></section>`;
const field = (label, inner, hint = "", full = false) =>
  `<div class="field${full ? " full" : ""}"><label>${label}</label>${inner}${hint ? `<div class="hint">${hint}</div>` : ""}</div>`;
const text = (id, v, ph = "", type = "text") => `<input type="${type}" id="${id}" value="${esc(v ?? "")}" placeholder="${esc(ph)}">`;
const check = (id, v, label) => `<label class="check"><input type="checkbox" id="${id}" ${v ? "checked" : ""}> ${label}</label>`;
const val = (id) => ($(id)?.value ?? "").trim();
const on = (id) => !!$(id)?.checked;
const lines = (id) => $(id).value.split("\n").map((s) => s.trim()).filter(Boolean);

// In auto mode, the theme editor's "Start from" follows Windows light/dark too (until you pick one).
onSystemThemeChange(() => {
  const b = document.getElementById("tBase");
  if (!b || b.dataset.touched) return;
  b.value = resolveTheme(S.cfg.settings).name;
  b.dispatchEvent(new Event("change"));
});

// ---------- settings backup ----------
// On Android the shell shows its own save/open screens and answers with an androidBackup event.
let backupWait = null;
let backupListening = false;
function androidBackup(start) {
  if (!backupListening) {
    backupListening = true;
    onEvent("androidBackup", (d) => { const r = backupWait; backupWait = null; r?.(d || {}); });
  }
  return new Promise((resolve) => { backupWait?.({ canceled: true }); backupWait = resolve; start(); });
}

function openBackupExport() {
  open(`<div class="modal-head">Export settings</div>
    <div class="modal-body">
      <p class="hint" style="margin:0 0 12px">Your servers, shortcuts, settings, nick colours, plugins and custom themes go into one .json file.</p>
      ${check("bkPw", false, "Include passwords (server, SASL / NickServ and bouncer)")}
      <div id="bkPwBox" class="hidden" style="margin-top:10px">
        <div class="fgrid">
          ${field("Passphrase", `<input type="password" id="bkPass" autocomplete="new-password" placeholder="at least 8 characters">`)}
          ${field("Passphrase again", `<input type="password" id="bkPass2" autocomplete="new-password">`)}
        </div>
        <div class="hint">The passwords are encrypted with this passphrase. You'll need it to import them, and it can't be recovered.</div>
      </div>
      <div class="hint" style="margin-top:10px">Without passwords you type them again after importing. Bot URL commands are saved as they are, so check them for API keys before sharing the file.</div>
      <div id="bkErr" class="t-status bad" style="margin-top:8px"></div>
    </div>
    <div class="modal-foot"><button class="btn" id="bkCancel">Cancel</button><button class="btn primary" id="bkOk">Export</button></div>`, "sm", true);
  $("bkPw").onchange = () => { $("bkPwBox").classList.toggle("hidden", !on("bkPw")); if (on("bkPw")) $("bkPass").focus(); };
  $("bkCancel").onclick = closeOverlay;
  $("bkOk").onclick = async () => {
    const err = (m) => { $("bkErr").textContent = m; };
    const withPw = on("bkPw");
    const pass = $("bkPass").value;
    if (withPw && pass !== $("bkPass2").value) return err("The passphrases don't match.");
    $("bkOk").disabled = true;
    err("");
    const res = await Go.ExportSettings(withPw, withPw ? pass : "");
    $("bkOk").disabled = false;
    if (!res || res.error) return err(res?.error || "Export failed.");
    if (android) {
      const d = await androidBackup(() => window.icbAndroid.saveBackup(res.name, res.data));
      if (d.canceled) return;
      if (d.error) return err(d.error);
    } else {
      const e = await Go.SaveBackup(res.name, res.data);
      if (e) return err(e);
    }
    closeOverlay();
    toast("Settings exported", "ok");
  };
}

async function openBackupImport() {
  let data = "";
  if (android) {
    const d = await androidBackup(() => window.icbAndroid.openBackup());
    if (d.canceled) return;
    if (d.error) return toast(d.error, "error");
    data = d.data || "";
  } else {
    const f = await Go.OpenBackup();
    if (f?.error) return toast(f.error, "error");
    data = f?.data || "";
  }
  if (!data) return;
  const info = await Go.InspectSettings(data);
  if (!info || info.error) return toast(info?.error || "That isn't an IRChatterbox settings backup.", "error");
  const when = info.exported ? new Date(info.exported).toLocaleString() : "";
  const n = (k, one, many) => `${k} ${k === 1 ? one : many}`;
  open(`<div class="modal-head">Import settings</div>
    <div class="modal-body">
      <p style="margin:0 0 10px">Backup${info.version ? ` from IRChatterbox ${esc(info.version)}` : ""}${when ? `, made ${esc(when)}` : ""}:
        ${n(info.servers, "server", "servers")}, ${n(info.shortcuts, "shortcut", "shortcuts")}, ${n(info.themes, "custom theme", "custom themes")}.</p>
      <p class="hint" style="margin:0 0 10px">Servers in the backup replace the same servers here, and new ones are added. Shortcuts, settings and plugins are replaced; nick colours and themes are added to yours.</p>
      ${info.hasPasswords ? field("Passphrase", `<input type="password" id="biPass" autocomplete="off">`, "This backup has encrypted passwords. Leave it empty to import without them.") :
        `<p class="hint" style="margin:0">This backup has no passwords. Servers you already have keep theirs.</p>`}
      <div id="biErr" class="t-status bad" style="margin-top:8px"></div>
    </div>
    <div class="modal-foot"><button class="btn" id="biCancel">Cancel</button><button class="btn primary" id="biOk">Import</button></div>`, "sm", true);
  $("biCancel").onclick = closeOverlay;
  const go = async () => {
    $("biOk").disabled = true;
    $("biErr").textContent = "";
    const e = await Go.ImportSettings(data, $("biPass")?.value || "");
    $("biOk").disabled = false;
    if (e) { $("biErr").textContent = e; $("biPass")?.focus(); return; }
    closeOverlay();
    toast("Settings imported", "ok");
    setTimeout(() => location.reload(), 600);
  };
  $("biOk").onclick = go;
  $("biPass")?.addEventListener("keydown", (e) => { if (e.key === "Enter") go(); });
  ($("biPass") || $("biOk")).focus();
}

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
export function openPasteConfirm(lines, target, done, oneMessage = false) {
  const n = lines.length;
  const secs = Math.max(0, Math.ceil((n - 5) / 2));
  const preview = lines.slice(0, 6).map((l) => esc(l.length > 140 ? l.slice(0, 140) + "…" : l)).join("\n") + (n > 6 ? `\n… and ${n - 6} more` : "");
  const note = oneMessage
    ? "This server supports multi-line messages, so it arrives as one message with its line breaks, without flooding the channel."
    : `Sending many lines at once can get you kicked for flooding.${secs ? ` IRChatterbox paces them, so this takes about ${secs} second${secs === 1 ? "" : "s"}.` : ""}`;
  open(`<div class="modal-head">Send ${n} lines to ${esc(target)}?</div>
    <div class="modal-body">
      <p style="margin:0 0 10px" class="muted">${note}</p>
      <pre class="paste-preview">${preview}</pre>
      <p class="hint" style="margin:10px 0 0">Upload as a link puts the text on paste.rs, a free public paste site, and sends just the link. Anyone with the link can read it.</p>
    </div>
    <div class="modal-foot"><button class="btn left" id="pcCancel">Cancel</button><button class="btn" id="pcLink">Upload as a link</button><button class="btn" id="pcJoin">Join into one line</button><button class="btn primary" id="pcSend">${oneMessage ? "Send as one message" : `Send ${n} lines`}</button></div>`, "sm paste-dlg", true);
  const finish = (mode) => { closeOverlay(); done(mode); };
  $("pcCancel").onclick = () => finish(null);
  $("pcLink").onclick = () => finish("link");
  $("pcJoin").onclick = () => finish("join");
  $("pcSend").onclick = () => finish("send");
  $("pcSend").focus();
}

// ---------- report ----------
// info: { nick, where, message }. send({reason, note, block}) resolves, or throws with a message.
const REPORT_REASONS = [["spam", "Spam or scam"], ["harassment", "Harassment or bullying"], ["hate", "Hate speech"],
  ["sexual", "Sexual content"], ["illegal", "Illegal content or threats"], ["other", "Something else"]];
export function openReportDialog(info, send) {
  open(`<div class="modal-head">Report ${esc(info.nick)}</div>
    <div class="modal-body">
      ${info.message ? `<div class="report-msg"><b>${esc(info.nick)}</b> in ${esc(info.where)}: ${esc(info.message.length > 300 ? info.message.slice(0, 300) + "…" : info.message)}</div>` : ""}
      <div class="fgrid">
        ${field("Reason", `<select id="rpReason">${REPORT_REASONS.map(([v, l]) => `<option value="${v}">${l}</option>`).join("")}</select>`, "", true)}
        ${field("Details (optional)", `<textarea id="rpNote" rows="3" maxlength="1000" placeholder="Anything that helps us understand what happened"></textarea>`, "", true)}
      </div>
      ${check("rpBlock", true, `Also block ${esc(info.nick)} (you won't see their messages)`)}
      <p class="hint" style="margin:10px 0 0">The report, with the last few messages around it, goes to the IRChatterbox team for review. IRC networks are run independently, so the channel's operators and the network's staff can act on it too.</p>
      <p class="hidden" id="rpErr"></p>
    </div>
    <div class="modal-foot"><button class="btn" id="rpCancel">Cancel</button><button class="btn primary" id="rpSend">Send report</button></div>`, "sm", true);
  $("rpCancel").onclick = closeOverlay;
  $("rpSend").onclick = async () => {
    $("rpSend").disabled = true;
    $("rpSend").textContent = "Sending…";
    $("rpErr").classList.add("hidden");
    try {
      await send({ reason: $("rpReason").value, note: $("rpNote").value.trim(), block: on("rpBlock") });
      closeOverlay();
    } catch (e) {
      $("rpErr").textContent = e.message || "The report couldn't be sent";
      $("rpErr").classList.remove("hidden");
      $("rpSend").disabled = false;
      $("rpSend").textContent = "Try again";
    }
  };
}

// ---------- quit confirmation ----------
let quitKeys = null;
export function openQuitConfirm(connected, bouncer) {
  if (quitKeys) document.removeEventListener("keydown", quitKeys, true);
  const list = connected || [], kept = bouncer || [];
  const names = (l) => l.length === 1 ? `<b>${esc(l[0])}</b>` : `<b>${l.slice(0, -1).map(esc).join("</b>, <b>")}</b> and <b>${esc(l[l.length - 1])}</b>`;
  let what = list.length ? `You'll be disconnected from ${names(list)}.` : kept.length ? "" : "You're not connected to any servers.";
  if (kept.length) what += `${what ? " " : ""}Your bouncer keeps you online on ${names(kept)} and saves what you miss.`;
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
  const c = { name: "", host: "", port: 6697, tls: true, nick: "", authMethod: "none", autoConnect: true, rememberChannels: true, autoJoin: [], perform: [], bouncer: "", ...(cfg || {}) };
  if (!c.nick) c.nick = S.servers.find((s) => s.cfg.nick)?.cfg.nick || (await Go.GetDefaultNick()) || "";
  open(`<div class="modal-head">${isNew ? "Add server" : "Edit " + esc(c.name)}</div>
    <div class="tabs se-tabs">${[["conn", "Connection"], ["id", "Identity & login"], ["chan", "Channels"], ["adv", "Advanced"]]
      .map(([k, l], i) => `<button class="tab ${i ? "" : "on"}" data-st="${k}">${l}</button>`).join("")}</div>
    <div class="modal-body se-body ${c.bouncer ? "bnc-mode" : ""}" id="sBody">
      <div class="st-panel" data-panel="conn">
      <div class="conn-mode">
        <button class="mode-opt ${c.bouncer ? "" : "on"}" data-mode="direct"><b>Direct</b><span>Connect straight to the network. You're online while IRChatterbox is open.</span></button>
        <button class="mode-opt ${c.bouncer ? "on" : ""}" data-mode="bouncer"><b>Through a bouncer</b><span>soju or ZNC keeps you online 24/7 and gives you what you missed.</span></button>
      </div>
      ${isNew ? `<div class="only-direct"><div class="section-title">Popular networks</div>
      <div class="chips presets">${PRESETS.map(([n, h]) => `<button class="chip preset" data-host="${h}" data-name="${n}">${n}</button>`).join("")}</div></div>` : ""}
      <div class="section-title"><span class="only-direct">Connection</span><span class="only-bouncer">Bouncer</span></div>
      <div class="fgrid">
        ${field("Name", text("sName", c.name, "Libera.Chat"))}
        ${field("Host", text("sHost", c.host, "irc.libera.chat"))}
        ${field("Port", text("sPort", c.port, "6697", "number"))}
        <div class="field"><label>&nbsp;</label>${check("sTls", c.tls, "Use TLS (SSL)")}</div>
        <div class="field only-bouncer"><label>Bouncer type</label><select id="sBnc">
            <option value="soju" ${c.bouncer !== "znc" ? "selected" : ""}>soju</option>
            <option value="znc" ${c.bouncer === "znc" ? "selected" : ""}>ZNC</option></select></div>
        <div class="field only-bouncer"><label>Network</label>${text("sBncNet", c.bouncerNetwork, "libera")}<div class="hint">The network's name on your bouncer. Add one server here per network.</div></div>
        <div class="field only-bouncer"><label>Bouncer username</label>${text("sBncUser", c.bouncerUser, "your bouncer login")}</div>
        <div class="field only-bouncer"><label>Bouncer password</label>${text("sBncPass", c.bouncerPass, "", "password")}</div>
        <div class="field full only-bouncer enc-field"><label for="sEncKey">History decryption key <span class="muted">(optional)</span></label>
          <textarea id="sEncKey" rows="2" spellcheck="false" autocomplete="off" placeholder="AGE-SECRET-KEY-1…">${esc(c.encryptionKey || "")}</textarea>
          <div class="enc-row"><button type="button" class="btn sm" id="sEncGen">Generate a key</button><span id="sEncPub"></span></div>
          <div class="hint">Only needed if your bouncer encrypts stored history. This private key stays on this device and is never sent anywhere. Give your bouncer the <b>public</b> key shown above, on your account page or with <code>/msg BouncerServ user update -encryption-key age1…</code>. Keep a copy somewhere safe: without it, encrypted history can't be read.</div></div>
        <div class="field only-direct"><label>Server password</label>${text("sPass", c.password, "usually empty", "password")}</div>
        <div class="field"><label>&nbsp;</label>${check("sAuto", c.autoConnect, "Connect automatically on startup")}</div>
      </div>
      </div>
      <div class="st-panel hidden" data-panel="id">
      <div class="section-title">Identity</div>
      <div class="fgrid">
        ${field("Nickname", text("sNick", c.nick, "Andrew"))}
        ${field("Alternate nickname", text("sAlt", c.altNick, "Andrew_"))}
        <div class="field only-direct"><label>Username (ident)</label>${text("sUser", c.username, "same as nick")}</div>
        ${field("Real name", text("sReal", c.realname, "IRChatterbox user"))}
      </div>
      <div class="hint only-bouncer bnc-note">Logging in to the network itself (SASL or NickServ) is set up on the bouncer, not here.</div>
      <div class="section-title only-direct">Automatic login</div>
      <div class="fgrid only-direct">
        ${field("Method", `<select id="sAuthM">
            <option value="none" ${c.authMethod === "none" ? "selected" : ""}>None</option>
            <option value="sasl" ${c.authMethod === "sasl" ? "selected" : ""}>SASL PLAIN (recommended)</option>
            <option value="nickserv" ${c.authMethod === "nickserv" ? "selected" : ""}>NickServ IDENTIFY</option>
            <option value="external" ${c.authMethod === "external" ? "selected" : ""}>Client certificate (SASL EXTERNAL)</option></select>`)}
        <div></div>
        ${field("Account name", text("sAuthU", c.authUser, "defaults to nickname"))}
        ${field("Password", text("sAuthP", c.authPass, "", "password"))}
      </div>
      <div class="section-title only-direct">Client certificate (CertFP)</div>
      <div class="only-direct cert-box">
        <div id="sCertInfo" class="cert-info"></div>
        <div class="btn-row"><button class="btn sm" id="sCertGen">Create a certificate</button><button class="btn sm" id="sCertImp">Import…</button>
          <button class="btn sm" id="sCertCopy">Copy fingerprint</button><button class="btn sm danger" id="sCertDel">Remove</button></div>
        <div class="hint">Log in without sending a password. Create a certificate, save and connect (with TLS on), log in with your password once, then run <code>/msg NickServ CERT ADD</code>. After that, choose “Client certificate” as the method above. The private key is kept with your passwords.</div>
      </div>
      </div>
      <div class="st-panel hidden" data-panel="chan">
      <div class="section-title">Channels & commands</div>
      <div class="fgrid">
        ${field("Auto-join channels", `<textarea id="sJoin" placeholder="#linux&#10;#secret key">${esc((c.autoJoin || []).join("\n"))}</textarea>`, "One per line. Add a key after a space for keyed channels.")}
        ${field("Perform on connect", `<textarea id="sPerform" placeholder="/mode $me +x&#10;/wait 3&#10;/msg InviteBot !invite">${esc((c.perform || []).join("\n"))}</textarea>`, "Commands run in order after connecting. $me = your nick, /wait 3 pauses 3 seconds. Invites to your auto-join channels are joined automatically.")}
        <div class="full">${check("sRemember", c.rememberChannels, "Remember channels I join and leave (keeps the auto-join list up to date)")}</div>
      </div>
      </div>
      <div class="st-panel hidden" data-panel="adv">
      <div class="section-title">Server icon</div>
      <div class="se-icon"><span class="se-icon-pic" id="sIconPic"></span>
        <button class="btn sm" id="sIconPick">Choose…</button><button class="btn sm" id="sIconDel">Remove</button>
        <span class="hint" style="margin:0">Shown in the sidebar. Only you can see it.</span></div>
      <div class="se-letters"><label for="sIconTxt">Letters</label><input id="sIconTxt" class="no-i18n" maxlength="4" spellcheck="false" autocomplete="off" value="${esc(c.iconText || "")}">
        <span class="hint" style="margin:0">Up to 2, used when there's no picture.</span></div>
      <div class="field full">${check("sLetterIco", c.letterIcon, "Show the letters in the sidebar when there's no picture")}</div>
      <div class="section-title">Chat</div>
      <div class="fgrid">
        ${field("Joins, parts and quits", `<select id="sJoins">
          <option value="">${S.cfg.settings.showJoinsParts === false ? "As in Settings (hidden)" : "As in Settings (shown)"}</option>
          <option value="show" ${c.joinsParts === "show" ? "selected" : ""}>Show</option>
          <option value="hide" ${c.joinsParts === "hide" ? "selected" : ""}>Hide</option></select>`, "For one channel, right-click it in the sidebar.", true)}
      </div>
      <div class="section-title">Connection options</div>
      <div class="fgrid">
        ${field("Text encoding", `<select id="sEnc"></select>`, "Older networks may use a regional encoding. Automatic suits almost every server.", true)}
        <div class="field full">${check("sSkip", c.tlsSkipVerify, "Accept invalid certificates")}<div class="hint indent">Only for servers with a self-signed certificate that you trust.</div></div>
      </div>
      <div class="section-title">Proxy</div>
      <div class="fgrid">
        ${field("Connect through", `<select id="sPxType">
          <option value="">No proxy</option>
          <option value="socks5" ${c.proxyType === "socks5" ? "selected" : ""}>SOCKS5 proxy (Tor, SSH, VPN apps)</option>
          <option value="http" ${c.proxyType === "http" ? "selected" : ""}>HTTP proxy</option></select>`, "", true)}
        <div class="field full px-row"><div class="px-addr">
          ${field("Proxy host", text("sPxHost", c.proxyHost, "127.0.0.1"))}
          ${field("Port", text("sPxPort", c.proxyPort || "", "9050", "number"))}</div></div>
        <div class="px-row">${field("Proxy user name", text("sPxUser", c.proxyUser, "Optional"))}</div>
        <div class="px-row">${field("Proxy password", `<input type="password" id="sPxPass" value="${esc(c.proxyPass || "")}" placeholder="Optional" autocomplete="off">`)}</div>
        <div class="hint full px-row">Tor is usually 127.0.0.1 port 9050 (Tor Browser: 9150). Link previews and DCC transfers don't go through the proxy.</div>
      </div>
      </div>
    </div>
    <div class="modal-foot">
      ${isNew ? "" : `<button class="btn danger left" id="sDel">Remove</button>`}
      <span class="form-err ${isNew ? "left" : ""}" id="sErr"></span>
      <button class="btn" id="sCancel">Cancel</button>
      <button class="btn" id="sSave">Save</button>
      <button class="btn primary" id="sSaveC">Save & connect</button>
    </div>`, "", true);
  // tabs: every field stays in the form, only one panel shows at a time
  const showTab = (k) => {
    document.querySelectorAll(".se-tabs .tab").forEach((t) => t.classList.toggle("on", t.dataset.st === k));
    document.querySelectorAll("#sBody .st-panel").forEach((p) => p.classList.toggle("hidden", p.dataset.panel !== k));
  };
  document.querySelector(".se-tabs").onclick = (e) => { const t = e.target.closest("[data-st]"); if (t) showTab(t.dataset.st); };
  // server icon: picked here, saved with the server
  let icon; // undefined = unchanged, "" = remove, data URL = new
  // client certificate: { cert, key, fingerprint, subject, expires } or null
  let cert = null;
  const drawCert = () => {
    const box = $("sCertInfo");
    const has = !!cert?.cert;
    box.innerHTML = has
      ? `<b class="no-i18n">${esc(cert.subject || "")}</b> · <span>expires</span> <span class="no-i18n">${esc(cert.expires || "")}</span><br><span class="no-i18n mono">SHA-256 ${esc(cert.fingerprint || "")}</span>`
      : `<span class="muted">None</span>`;
    $("sCertCopy").classList.toggle("hidden", !has);
    $("sCertDel").classList.toggle("hidden", !has);
    $("sCertGen").textContent = has ? "Create a new one" : "Create a certificate";
  };
  if (c.clientCert) Go.DescribeClientCert(c.clientCert, c.clientKey || "").then((d) => { cert = d?.cert ? d : { cert: c.clientCert, key: c.clientKey, subject: d?.error || "" }; drawCert(); });
  setTimeout(drawCert);
  $("sCertGen").onclick = async () => {
    if (cert?.cert && !confirm(t("Replace this certificate? Services that know its fingerprint won't recognise the new one until you add it again."))) return;
    const d = await Go.GenerateClientCert(val("sNick") || c.nick || "");
    if (d?.error) { $("sErr").textContent = d.error; return; }
    cert = d; drawCert();
  };
  $("sCertImp").onclick = async () => {
    const d = await Go.ImportClientCert();
    if (d?.error) { $("sErr").textContent = d.error; return; }
    if (d?.cert) { cert = d; drawCert(); }
  };
  $("sCertCopy").onclick = () => cert?.fingerprint && navigator.clipboard.writeText(cert.fingerprint);
  $("sCertDel").onclick = () => { cert = null; drawCert(); if ($("sAuthM").value === "external") $("sAuthM").value = "none"; };
  const drawIcon = () => {
    const cur = icon !== undefined ? icon : (c.id ? S.icons?.[c.id + "/*"] || "" : "");
    const name = val("sName") || val("sHost") || "?";
    const pic = $("sIconPic");
    pic.innerHTML = cur ? `<img src="${cur}" alt="">` : esc(iconLetters($("sIconTxt").value, name));
    pic.classList.toggle("letters", !cur);
    pic.style.setProperty("--tile", nickColor(name));
    $("sIconTxt").placeholder = iconLetters("", name);
    $("sIconDel").classList.toggle("hidden", !cur);
  };
  $("sIconTxt").addEventListener("input", drawIcon);
  $("sName").addEventListener("input", drawIcon);
  $("sIconPick").onclick = () => pickSquareImage((url) => { icon = url; drawIcon(); });
  $("sIconDel").onclick = () => { icon = ""; drawIcon(); };
  drawIcon();
  document.querySelectorAll(".preset").forEach((b) => (b.onclick = () => {
    $("sHost").value = b.dataset.host; $("sName").value = b.dataset.name;
    $("sHost").classList.remove("invalid"); $("sErr").textContent = "";
    $("sTls").checked = true; $("sPort").value = 6697;
    document.querySelectorAll(".preset").forEach((x) => x.classList.toggle("on", x === b));
    $("sNick").focus();
  }));
  const setMode = (bnc) => {
    $("sBody").classList.toggle("bnc-mode", bnc);
    document.querySelectorAll(".mode-opt").forEach((x) => x.classList.toggle("on", (x.dataset.mode === "bouncer") === bnc));
    $("sHost").placeholder = bnc ? "bouncer.example.com" : "irc.libera.chat";
    $("sName").placeholder = bnc ? "Libera (bouncer)" : "Libera.Chat";
  };
  document.querySelectorAll(".mode-opt").forEach((b) => (b.onclick = () => { setMode(b.dataset.mode === "bouncer"); $("sHost").focus(); }));
  setMode(!!c.bouncer);
  const pxSync = () => document.querySelectorAll(".px-row").forEach((r) => r.classList.toggle("hidden", !$("sPxType").value));
  $("sPxType").addEventListener("change", () => { pxSync(); if ($("sPxType").value && !val("sPxHost")) { $("sPxHost").value = "127.0.0.1"; $("sPxPort").value = $("sPxType").value === "socks5" ? "9050" : "8080"; } });
  pxSync();
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
  // ---- history decryption key ----
  const encSync = async () => {
    const k = $("sEncKey").value.trim();
    const out = $("sEncPub");
    out.textContent = "";
    out.className = "";
    if (!k) return true;
    try {
      const pubs = (await Go.EncryptionPublicKeys(k)) || [];
      out.innerHTML = `Public key: <code id="sEncPubKey">${esc(pubs[pubs.length - 1] || "")}</code> <button type="button" class="btn sm" id="sEncCopy">Copy</button>`
        + (pubs.length > 1 ? ` <span class="muted">(+${pubs.length - 1} older)</span>` : "");
      $("sEncCopy").onclick = async () => {
        try { await navigator.clipboard.writeText($("sEncPubKey").textContent); $("sEncCopy").textContent = "Copied"; } catch {}
      };
      return true;
    } catch (e) {
      out.className = "err";
      out.textContent = "That isn't an age private key. It starts with AGE-SECRET-KEY-1.";
      return false;
    }
  };
  $("sEncKey").addEventListener("change", encSync);
  $("sEncGen").onclick = async () => {
    const pair = await Go.GenerateEncryptionKey();
    if (!pair?.privateKey) return;
    // keep any existing key on its own line, so history encrypted with it stays readable
    const cur = $("sEncKey").value.trim();
    $("sEncKey").value = cur ? cur + "\n" + pair.privateKey : pair.privateKey;
    await encSync();
  };
  encSync();

  const save = async (connect) => {
    if (!(await encSync())) {
      showTab($("sEncKey").closest(".st-panel").dataset.panel);
      $("sErr").textContent = "Fix the history decryption key, or clear it";
      return;
    }
    const bnc = $("sBody").classList.contains("bnc-mode");
    const missing = !val("sHost") ? "sHost" : !val("sNick") ? "sNick" : bnc && !val("sBncUser") ? "sBncUser" : bnc && !$("sBncPass").value ? "sBncPass" : null;
    document.querySelectorAll(".field input.invalid").forEach((x) => x.classList.remove("invalid"));
    if (missing) {
      showTab($(missing).closest(".st-panel").dataset.panel);
      $(missing).classList.add("invalid");
      $(missing).focus();
      $("sErr").textContent = missing === "sHost" ? (bnc ? "Enter your bouncer's host" : "Enter a server host (or pick a network above)")
        : missing === "sNick" ? "Enter a nickname" : missing === "sBncUser" ? "Enter your bouncer username" : "Enter your bouncer password";
      return;
    }
    const out = {
      ...c, name: val("sName") || val("sHost"), host: val("sHost"), port: parseInt(val("sPort")) || 6697, tls: on("sTls"), tlsSkipVerify: on("sSkip"),
      password: $("sPass").value, nick: val("sNick"), altNick: val("sAlt"), username: val("sUser"), realname: val("sReal"),
      authMethod: $("sAuthM").value, authUser: val("sAuthU"), authPass: $("sAuthP").value, autoConnect: on("sAuto"),
      rememberChannels: on("sRemember"), autoJoin: lines("sJoin"), perform: lines("sPerform"), encoding: $("sEnc").value,
      bouncer: bnc ? $("sBnc").value : "", bouncerUser: val("sBncUser"), bouncerPass: $("sBncPass").value, bouncerNetwork: val("sBncNet"),
      encryptionKey: $("sEncKey").value.trim(),
      iconText: Array.from($("sIconTxt").value.replace(/\s+/g, "")).slice(0, 2).join(""), letterIcon: on("sLetterIco"), joinsParts: $("sJoins").value,
      proxyType: $("sPxType").value, proxyHost: val("sPxHost"), proxyPort: parseInt(val("sPxPort")) || 0, proxyUser: val("sPxUser"), proxyPass: $("sPxPass").value,
    };
    out.clientCert = cert?.cert || "";
    out.clientKey = cert?.cert ? cert.key || "" : "";
    if (out.authMethod === "external" && !bnc && (!out.clientCert || !out.tls)) {
      showTab("id");
      $("sErr").textContent = !out.clientCert ? "Create or import a client certificate to log in with it" : "Logging in with a certificate needs TLS (Connection tab)";
      return;
    }
    if (out.proxyType && (!out.proxyHost || !out.proxyPort)) {
      showTab("adv");
      $("sErr").textContent = "Enter the proxy's host and port, or choose No proxy";
      return;
    }
    const saved = await Go.SaveServer(out);
    if (!saved?.id) { $("sErr").textContent = "Couldn't save the server"; return; }
    if (icon !== undefined) await Go.SetChannelIcon(saved.id, "*", icon);
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

// pickSquareImage lets you pick a picture and hands back a 128×128 PNG data URL, cropped to the middle.
function pickSquareImage(done) {
  const inp = document.createElement("input");
  inp.type = "file";
  inp.accept = "image/png,image/jpeg,image/webp,image/gif,image/bmp";
  inp.onchange = async () => {
    const f = inp.files?.[0];
    if (!f) return;
    try {
      const img = await createImageBitmap(f);
      const side = Math.min(img.width, img.height), cv = document.createElement("canvas");
      cv.width = cv.height = 128;
      const ctx = cv.getContext("2d");
      ctx.imageSmoothingQuality = "high";
      ctx.drawImage(img, (img.width - side) / 2, (img.height - side) / 2, side, side, 0, 0, 128, 128);
      done(cv.toDataURL("image/png"));
    } catch { toast("That picture couldn't be opened", "error"); }
  };
  inp.click();
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
    <div class="modal-foot"><button class="btn left desktop-only" id="stFolder">Open data folder</button><button class="btn" id="stCancel">Cancel</button><button class="btn primary" id="stSave">Save changes</button></div>`, "settings");
  m.querySelector(".tabs").onclick = (e) => {
    const t = e.target.closest("[data-t]");
    if (!t) return;
    collect();
    tab = t.dataset.t;
    m.querySelectorAll(".tab").forEach((x) => x.classList.toggle("on", x === t));
    draw();
  };
  $("stX").onclick = $("stCancel").onclick = () => { applySettings(); setLanguage(S.cfg.settings.language || ""); closeOverlay(); };
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
          ${android ? "" : `<option value="file" ${(value || "").startsWith("file:") ? "selected" : ""}>Custom sound file…</option>`}
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
      ${check("gJoins", set.showJoinsParts !== false, "Show joins, parts and quits")}
      ${check("gMarker", set.markerLine !== false, "Mark unread messages with a divider line")}
      ${check("gCompact", set.compactMode, "Compact line spacing")}
      ${check("gUsers", S.ui.users !== false, "Show the user list")}`) +
      card("History & logging", "Manage how much chat history is kept and whether to save it to files.", `
      ${check("gLog", set.logging, "Keep chat history between restarts")}
      <div class="fgrid" style="margin-top:10px">
        ${field("History shown when opening a channel", text("gHist", set.historyLines, "200", "number"))}
        ${field("Maximum saved lines per channel", text("gMax", set.logMaxLines ?? 10000, "10000", "number"), "Older lines are removed from the history and .txt files. 0 = keep everything.")}
      </div>
      <div class="desktop-only"><div class="set-sep"></div>
      ${check("gTxt", set.textLogs, "Save each channel to a text log file (.txt)")}
      <div class="hint indent">Readable logs like <code>[2026-10-16 09:41:22] &lt;@Andrew&gt; hello</code> — one file per channel and private chat.</div>
      <div class="field" id="gTxtRow" style="margin-top:10px"><label>Log folder</label>
        <div style="display:flex;gap:8px"><input type="text" id="gFolder" value="${esc(set.logFolder || "")}" placeholder="Default: the IRChatterbox data folder\\logs" style="flex:1">
        <button class="btn sm" id="gBrowse">Browse…</button><button class="btn sm" id="gOpenLogs">Open</button></div></div></div>`) +
      card("Moderation", "For channels where you're an operator.", `
      ${check("gModConfirm", set.confirmModeration !== false, "Ask before kicking or banning someone, with a box for the reason")}
      <div class="fgrid" style="margin-top:10px">
        ${field("Default kick reason", text("gKickReason", set.kickReason || "", "Their nick"), "Used when you leave the reason empty, or when you aren't asked.")}
      </div>`) +
      card("Startup & closing", "Configure how IRChatterbox starts and exits.", `
      <div class="win-only">${check("gAuto", set.startWithWindows, "Launch when I sign in to Windows")}
      <div class="indent">${check("gMin", set.startMinimized, "Start minimised to the taskbar")}</div></div>
      ${check("gConfirm", set.confirmOnClose, "Confirm before quitting when I close the window")}
      <div class="win-only">${check("gTray", set.trayIcon !== false, "Show an icon in the system tray")}
      <div class="indent">${check("gCloseTray", set.closeToTray, "Closing the window hides it to the tray (quit from the tray menu)")}</div></div>`, "desktop-only") +
      card("Background", "What happens when you leave the app.", `
      ${check("gKeep", set.keepAlive !== false, "Keep connection live in background")}
      <div class="hint indent">Stays connected while the app is closed, with a small “Connected” notification. Off: disconnects in the background, and your bouncer replays what you missed when you come back.</div>
      <div class="set-sep"></div>
      <div class="status-row"><span class="grow" id="gBatt"></span><button class="btn sm" id="gBattBtn">Allow</button></div>
      <div class="hint" id="gBattHint">Android may cut the connection to save battery. Letting IRChatterbox run unrestricted keeps it reliable.</div>
      <div class="status-row"><span class="grow" id="gNotif"></span><button class="btn sm" id="gNotifBtn">Settings</button></div>`, "android-only") +
      card("Away", android ? "Mark yourself away on every network when you put the app away." : "Mark yourself away on every network when you step away from your PC.", `
      ${check("gAway", set.autoAway, "Set me away automatically when I'm idle")}
      <div class="fgrid" style="margin-top:10px">
        ${field(android ? "After this many minutes in the background" : "After this many minutes without keyboard or mouse input", text("gAwayMin", set.autoAwayMinutes ?? 15, "15", "number"))}
        ${field("Away message", text("gAwayMsg", set.autoAwayMessage ?? "Away from keyboard", "Away from keyboard"))}
      </div>
      <div class="hint">You're set back as soon as you ${android ? "open the app" : "use your PC"} again. A manual <code>/away</code> is never undone automatically.</div>`) +
      card("Sending", "Protection against flooding a channel by accident.", `
      <div class="fgrid">${field("Ask before sending a paste of this many lines or more", text("gPaste", set.pasteWarnLines ?? 5, "5", "number"), "0 = never ask. You can send it as-is, join it into one line, or cancel.")}</div>
      <div class="set-sep"></div>
      ${check("gTyping", set.sendTyping !== false, "Let others see when I'm typing")}
      ${check("gSpell", set.spellCheck !== false, "Check spelling as I type (right-click a word for suggestions)")}
      ${check("gMention", set.mentionPopup !== false, "Typing @ shows the people in the channel to pick from")}
      <div class="hint indent">Shows “Andrew is typing…” in clients that support it, on servers that allow it.</div>`) +
      card("Messages", "The default message sent when you quit or leave a channel.", `
      <div class="fgrid">${field("Quit / part message", text("gQuit", set.quitMessage, "IRChatterbox"), "", true)}</div>`);
    else if (tab === "appearance") drawAppearance();
    else if (tab === "highlights") body.innerHTML =
      card("Highlight words", "Lines containing your nick or these words are highlighted, badged in red and trigger a notification.", `
      ${field("Words", `<textarea id="hWords" placeholder="andrew&#10;tshirtgang">${esc((set.highlightWords || []).join("\n"))}</textarea>`, "One per line. Your current nick is always a highlight.")}`) +
      card(android || os !== "windows" ? "Notifications" : "Windows notifications", "Pop-up alerts when someone needs your attention.", `
      ${check("hNotify", set.notifications, "Notify me when I'm highlighted and not looking at that channel")}
      ${check("hPm", set.notifyPMs, "Also notify for private messages")}
      <div class="hint">To get every message from a channel, or to mute one, right-click it in the sidebar and choose Notifications.</div>
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
      <div class="hint indent">Flashing stops as soon as you switch back to IRChatterbox. Tray icon options are in General → Startup & closing.</div>`, "desktop-only win-only") +
      card("Friends", "Get told when these people come online or go offline (on servers that support MONITOR).", `
      ${field("Nicks to watch", `<textarea id="hFriends" placeholder="Friendname1&#10;AndrewBot">${esc((set.notifyList || []).join("\n"))}</textarea>`, "One per line.")}
      ${check("hFriendsNotify", set.notifyFriends !== false, "Show a notification when one comes online")}`) +
      card("Blocked people", "Hide everything from these people. Block someone from their nick's menu, or with /ignore.", `
      ${field("Blocked nicks or masks", `<textarea id="hIgnore" placeholder="spammer&#10;*!*@bad.host">${esc((set.ignore || []).join("\n"))}</textarea>`, "Nick, or a nick!user@host mask with * wildcards.")}`) +
      card("Word filter", "Words you never want to see are replaced with •••• in chat, history and notifications.", `
      ${field("Filtered words", `<textarea id="hFilter" placeholder="badword&#10;spam*">${esc((set.filterWords || []).join("\n"))}</textarea>`, "One per line. Whole words only, any capitalisation; * matches the rest of a word. Your logs keep the original text.")}`);
    else if (tab === "shortcuts") drawShortcuts();
    else if (tab === "media") body.innerHTML =
      card("Inline media", "Show pictures and videos right in the chat.", `
      ${check("mImg", set.inlineImages, "Show images inline (PNG, JPG, GIF, WEBP, AVIF, SVG, imgur and Giphy links)")}
      ${check("mYt", set.inlineYouTube, "Show YouTube previews — click to play inside the chat")}
      ${check("mVid", set.inlineVideos ?? true, "Show video and audio players (MP4, WEBM, MOV, MP3, OGG, imgur GIFV) and Vimeo, Streamable or Spotify — click to play")}
      ${check("mLinks", set.linkPreviews !== false, "Show a preview under web links (title, description and picture)")}
      <div class="hint" style="margin-top:10px">Previews load directly from the linked site, which means that site sees your IP address. Turn these off if that matters to you.</div>`) +
      card("Image uploads", "Attach, paste or drop a JPG, PNG, WebP or BMP image in the message box to share it as a link.", `
      ${field("Upload to", `<select id="uHost">
        <option value="irchatterbox">IRChatterbox (i.irchatterbox.com)</option>
        <option value="imgur" ${set.uploadHost === "imgur" ? "selected" : ""}>Imgur</option>
        <option value="custom" ${set.uploadHost === "custom" ? "selected" : ""}>My own server</option>
      </select>`)}
      <div id="uHostHint" class="hint"></div>
      <div id="uImgurRow" style="margin-top:10px">
        ${field("Imgur Client ID", text("uImgur", set.imgurClientId, "e.g. 1a2b3c4d5e6f7g8"), `Register an app at <a href="#" data-open="https://api.imgur.com/oauth2/addclient">api.imgur.com</a> (choose "without a callback URL") and paste its Client ID here.`)}
      </div>
      <div id="uCustomRow" style="margin-top:10px">
        ${field("Upload address", text("uURL", set.uploadURL, "https://img.example.com/upload"), `Any server that takes the same request as irchatterbox.com. Run your own with <a href="https://github.com/phpandrew/IRChatterbox/tree/main/self-hosting" data-url="https://github.com/phpandrew/IRChatterbox/tree/main/self-hosting">self-hosting/upload.php</a> on GitHub.`)}
      </div>`) +
      card("DCC", "Send files and chat directly with someone, without going through the server. Right-click a nick and choose Send file… or Direct chat, or use /dcc.", `
      ${field("Save received files to", `<div style="display:flex;gap:8px"><input type="text" id="dFolder" value="${esc(set.dccFolder || "")}" placeholder="Default: your Downloads folder" style="flex:1"><button class="btn" id="dBrowse">Choose…</button></div>`)}
      <div class="fgrid">
        ${field("Your address", text("dAddr", set.dccAddress, "Detect automatically"), "Your public IP or host name, if detecting it gets it wrong.")}
        ${field("Listen on ports", text("dPorts", set.dccPorts, "Any free port"), "A port or range such as 5000-5010.")}
      </div>
      <div class="hint">Behind a home router, others can only reach your offers if these ports are forwarded to this computer. Files you receive are never opened automatically.</div>`, "desktop-only");
    else if (tab === "plugins") drawPlugins();
    else if (tab === "about") drawAbout();
    wire();
  }

  function wire() {
    if ($("uHost")) {
      const sync = () => {
        const imgur = $("uHost").value === "imgur";
        const custom = $("uHost").value === "custom";
        $("uImgurRow").style.display = imgur ? "" : "none";
        $("uCustomRow").style.display = custom ? "" : "none";
        $("uHostHint").textContent = imgur
          ? "Uploaded as-is to Imgur under your own Client ID. Imgur keeps images until they're deleted, and its own rules and limits apply."
          : custom ? "Uploaded to your server, which decides the size, format and how long images are kept."
          : "Shrunk to fit 1000×1000 and saved as WebP. Links work for 7 days, then the image is deleted. Anyone with the link can see it.";
      };
      $("uHost").addEventListener("change", sync); sync();
      document.querySelectorAll("#uImgurRow [data-open]").forEach((a) => (a.onclick = (e) => { e.preventDefault(); Go.OpenURL(a.dataset.open); }));
    }
    if ($("dBrowse")) $("dBrowse").onclick = async () => { const f = await Go.PickDCCFolder(); if (f) $("dFolder").value = f; };
    if ($("gTxt")) {
      const sync = () => { $("gTxtRow").style.opacity = on("gTxt") ? 1 : 0.5; };
      $("gTxt").addEventListener("change", sync); sync();
      $("gBrowse").onclick = async () => { const f = await Go.PickFolder("Choose where to save text logs"); if (f) $("gFolder").value = f; };
      $("gOpenLogs").onclick = () => Go.OpenLogFolder();
    }
    if ($("gBatt") && android) {
      const sync = () => {
        const ok = window.icbAndroid.batteryUnrestricted();
        $("gBatt").textContent = ok ? "✓ Battery: unrestricted" : "Battery: optimised by Android";
        $("gBattBtn").classList.toggle("hidden", ok);
        const n = window.icbAndroid.notificationsAllowed();
        $("gNotif").textContent = n ? "✓ Notifications allowed" : "Notifications are turned off";
        $("gNotifBtn").textContent = n ? "Settings" : "Turn on";
      };
      sync();
      $("gBattBtn").onclick = () => window.icbAndroid.requestUnrestricted();
      // Play build: the button opens the app's settings instead of asking directly
      Go.GetStore().then((store) => {
        if (store !== "play" || !$("gBattHint")) return;
        $("gBattBtn").textContent = "Open settings";
        $("gBattHint").textContent = "Android may cut the connection to save battery. In the app's settings, tap Battery (or App battery usage) and choose Unrestricted.";
      });
      $("gNotifBtn").onclick = () => (window.icbAndroid.notificationsAllowed() ? window.icbAndroid.openNotificationSettings() : window.icbAndroid.requestNotifications());
      // re-check when coming back from Android's settings screen
      document.addEventListener("visibilitychange", () => { if (!document.hidden && $("gBatt")) sync(); });
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
    const opts = (sel) => themeOptions(sel);
    body.innerHTML = card("Language", "Language of the app's menus and settings. Messages in chats are never translated.", `
      <div class="fgrid">${field("Language", `<select id="aLang"><option value="">Automatic (system language)</option>${LANGUAGES.map(([c, n]) => `<option value="${c}" ${set.language === c ? "selected" : ""}>${n}</option>`).join("")}</select>`)}</div>`) +
      card("Message layout", "How each message is laid out in the chat.", `
      <div class="fgrid">${field("Layout", `<select id="aLayout">
        <option value="">Automatic (stacked on phones, side by side on computers)</option>
        <option value="columns" ${set.chatLayout === "columns" ? "selected" : ""}>Side by side: time · name · message</option>
        <option value="stacked" ${set.chatLayout === "stacked" ? "selected" : ""}>Stacked: name and time above the message</option></select>`, "", true)}</div>`) +
      card("Theme", "Pick a theme, or follow Windows light and dark mode automatically.", `
      <div class="fgrid">
        ${field("Mode", `<select id="aMode">
          <option value="auto" ${set.theme === "auto" ? "selected" : ""}>Automatic — follow Windows light/dark</option>
          ${themeOptions(set.theme, (n) => "Always " + n)}</select>`, "", true)}
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
          const v = t.vars[k] || (k === "unread" ? t.vars.text : "") || "#000000";
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
    $("tBase").onchange = (e) => { if (e.isTrusted) $("tBase").dataset.touched = 1; $("tName").dataset.auto = 1; $("tName").value = ""; drawVars(); };
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
    $("aLang").onchange = () => setLanguage($("aLang").value); // preview now; kept when you save
    drawVars();
  }

  async function drawAbout() {
    const store = (await Go.GetStore().catch(() => "")) || "";
    body.innerHTML = `
      <section class="set-card about">
        <img class="about-logo for-light" src="${wordmarkLight}" alt="IRChatterbox">
        <img class="about-logo for-dark" src="${wordmarkDark}" alt="IRChatterbox">
        <p class="about-tag">A modern IRC client for Windows, macOS, Linux and Android.</p>
        <div class="about-ver">Version <b id="abVer">…</b></div>
        <p class="about-by">Created by <b>Andrew Lyle</b> · <a href="https://github.com/phpandrew" data-url="https://github.com/phpandrew">@phpandrew</a></p>
        <div class="about-actions">
          <button class="btn primary gh" data-open="${REPO}"><img src="${githubIcon}" alt="">View on GitHub</button>
          <button class="btn" data-open="${REPO}/releases">Releases</button>
          <button class="btn" data-open="${REPO}/issues">Report an issue</button>
        </div>
        <p class="about-support">Help and support: <a href="${REPO}/issues" data-url="${REPO}/issues">GitHub issues</a>. To report abuse in a chat, right-click (or long-press) the message or nick and choose <b>Report…</b></p>
        <a class="about-url" href="${REPO}" data-url="${REPO}">${REPO.replace("https://", "")}</a>
      </section>
      ${store === "play" ? card("Updates", "Updates are delivered by Google Play.", `
        <button class="btn sm" data-open="https://play.google.com/store/apps/details?id=io.github.phpandrew.irchatterbox">Open in Google Play</button>`) :
      card("Updates", "New versions are published on GitHub Releases.", `
        <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
          <button class="btn sm" id="abCheck">Check for updates</button>
          <span id="abStatus" class="muted" style="font-size:12.5px"></span>
        </div>
        <div style="margin-top:10px">${check("abAuto", set.autoUpdateCheck !== false, "Check for updates automatically")}</div>`)}
      ${card("Back up settings", "Move your setup to another computer or phone, or keep a copy.", `
        <div style="display:flex;gap:8px;flex-wrap:wrap">
          <button class="btn sm" id="abExport">Export settings…</button>
          <button class="btn sm" id="abImport">Import settings…</button>
        </div>
        <div class="hint" style="margin-top:8px">Saves your servers, shortcuts, settings, nick colours, plugins and custom themes to one file. Passwords are left out unless you choose to include them, and then they're encrypted with a passphrase. Save any changes in Settings first.</div>`)}
      ${card("Troubleshooting", "Something went wrong? The logs help track it down.", `
        <div style="display:flex;gap:8px;flex-wrap:wrap">
          <button class="btn sm" id="abLogs">Open logs</button>
          <button class="btn sm" id="abReport">Report a problem</button>
        </div>
        <div class="hint" style="margin-top:8px">“Report a problem” opens a new GitHub issue with your version and OS filled in. Attach <code>app.log</code> or a <code>crash-….log</code> from the logs folder if it helps — check it first, it can contain server and channel names.</div>`, "desktop-only")}
      <p class="about-foot">Built with Go and Wails · Emoji data from unicode-emoji-json (MIT)</p>`;
    body.querySelectorAll("[data-open]").forEach((b) => (b.onclick = () => Go.OpenURL(b.dataset.open)));
    $("abVer").textContent = (await Go.GetVersion()) || "dev";
    $("abLogs").onclick = () => Go.OpenDiagnostics();
    $("abExport").onclick = openBackupExport;
    $("abImport").onclick = openBackupImport;
    $("abReport").onclick = () => Go.ReportProblem();
    if ($("abCheck")) $("abCheck").onclick = async () => {
      const st = $("abStatus");
      st.className = "muted"; st.textContent = "Checking…";
      const info = await Go.CheckForUpdate();
      if (info.error) { st.className = "t-status bad"; st.textContent = info.error; return; }
      if (!info.available) { st.className = "t-status ok"; st.textContent = `✓ You're on the latest version (${info.current}).`; return; }
      st.className = "t-status ok";
      st.innerHTML = `Version ${esc(info.latest)} is available. <button class="btn sm primary" id="abInstall">Update &amp; restart</button>`;
      $("abInstall").textContent = android ? "Update" : info.manual ? "Download update" : "Update & restart";
      $("abInstall").onclick = () => { closeOverlay(); showUpdateBar(info); document.getElementById("ubInstall")?.click(); };
    };
  }

  function drawShortcuts() {
    body.innerHTML = card("Text shortcuts", "Type a shortcut and it's replaced with its text.", `
      <p class="hint" style="margin:0 0 12px">Type a shortcut in any message and it's replaced with its text, e.g. <code>$shrug</code> → <code>¯\\_(ツ)_/¯</code>. It expands when you press space or send, and typing <code>$</code> shows the list. The text can also be a command like <code>/me waves</code>. You can also manage shortcuts with <code>/shortcut add $name text</code>.</p>
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
      </div>
      <div class="btn-row"><button class="btn sm" id="muTest">Test</button></div>
      <div class="test-out hidden" id="muOut"></div>`, "desktop-only") +
      card("URL commands", "Typing the trigger fetches a web address and posts the result — like !solar.", `
      <div id="ucList"></div>
      <button class="btn sm" id="ucAdd">+ Add command</button>
      <div class="hint" style="margin-top:10px">Use <code>{args}</code> in the URL for text typed after the trigger. JSON replies can be shaped with a template like <code>Solar: {{.power}} W, today {{round .today_kwh 1}} kWh</code>.</div>`) +
      card("Script plugins", "Your own JavaScript plugins and mIRC scripts from the plugins folder.", `
      ${check("pScripts", plug.scriptsEnabled, "Run JavaScript (.js) and mIRC (.mrc) scripts from the plugins folder")}
      <div style="display:flex;gap:8px;margin-top:8px"><button class="btn sm desktop-only" id="pOpen">Open data folder</button><button class="btn sm" id="pReload">Reload plugins</button><span class="muted" id="pReloadOut" style="align-self:center;font-size:12px"></span></div>
      <div class="hint" style="margin-top:10px">See <code>plugins/_example.js</code> for the JavaScript API (on, reply, say, httpGet…) and <code>plugins/_example.mrc</code> for mIRC scripts. Type <code>/reload</code> after changing a file.</div>`, "desktop-only");
    const drawUc = () => {
      $("ucList").innerHTML = plug.urlCommands.map((u, i) => `
        <div class="urlcmd" data-i="${i}">
          <label class="check"><input type="checkbox" data-f="enabled" ${u.enabled ? "checked" : ""}> Enabled</label>
          <div class="indent"><label class="check"><input type="checkbox" data-f="onlyMe" ${u.onlyMe ? "checked" : ""}> Only I can trigger it</label></div>
          <div class="fgrid" style="margin-top:8px">
            ${field("Trigger", `<input type="text" data-f="trigger" value="${esc(u.trigger)}" placeholder="!solar">`)}
            ${field("Cooldown per channel (seconds)", `<input type="number" data-f="cooldown" value="${u.cooldown ?? 30}">`)}
            ${field("URL", `<input type="text" data-f="url" value="${esc(u.url)}" placeholder="https://example.com/solar.json">`, "", true)}
            ${field("Template (optional, for JSON)", `<input type="text" data-f="template" value="${esc(u.template)}" placeholder="Solar now: {{.power}} W">`, "", true)}
            ${field("Max lines", `<input type="number" data-f="maxLines" value="${u.maxLines || 3}">`)}
          </div>
          <div class="btn-row"><button class="btn sm" data-test="${i}">Test</button><button class="btn sm danger" data-rm="${i}">Remove</button></div>
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
      set.sendTyping = on("gTyping");
      if (has("gSpell")) set.spellCheck = on("gSpell");
      if (has("gMention")) set.mentionPopup = on("gMention");
      if (has("gModConfirm")) { set.confirmModeration = on("gModConfirm"); set.kickReason = val("gKickReason"); }
      if (has("gKeep")) set.keepAlive = on("gKeep");
      S.ui.users = on("gUsers");
      try { localStorage.setItem("ui", JSON.stringify(S.ui)); } catch {}
      Go.SaveUIState(JSON.stringify(S.ui));
    }
    if (has("aLang")) set.language = $("aLang").value;
    if (has("aLayout")) set.chatLayout = $("aLayout").value;
    if (has("aMode")) { set.theme = $("aMode").value; set.lightTheme = $("aLight").value; set.darkTheme = $("aDark").value; set.nickContrast = on("aContrast"); }
    if (has("hWords")) {
      set.highlightWords = lines("hWords"); set.notifications = on("hNotify"); set.notifyPMs = on("hPm"); set.ignore = lines("hIgnore"); set.filterWords = lines("hFilter");
      set.soundHighlight = readSound("sndHl"); set.soundPM = readSound("sndPm"); set.soundVolume = +$("sndVol").value;
      set.flashOnPing = on("hFlash");
      set.notifyList = lines("hFriends"); set.notifyFriends = on("hFriendsNotify");
    }
    if (has("abAuto")) set.autoUpdateCheck = on("abAuto");
    if (has("mImg")) { set.inlineImages = on("mImg"); set.inlineYouTube = on("mYt"); set.inlineVideos = on("mVid"); set.linkPreviews = on("mLinks"); }
    if (has("dFolder")) { set.dccFolder = val("dFolder"); set.dccAddress = val("dAddr"); set.dccPorts = val("dPorts"); }
    if (has("uHost")) { set.uploadHost = $("uHost").value; set.imgurClientId = val("uImgur"); set.uploadURL = val("uURL"); }
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
