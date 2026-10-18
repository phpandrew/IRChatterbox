// Browser-only mock backend used for UI development (not included in normal use).
const listeners = {};
export function on(ev, fn) { (listeners[ev] ||= []).push(fn); }
function emit(ev, data) { (listeners[ev] || []).forEach((f) => f(data)); }

const cfg = {
  servers: [
    { id: "lib", name: "Libera.Chat", host: "irc.libera.chat", port: 6697, tls: true, nick: "Andrew", authMethod: "sasl", autoConnect: true, autoJoin: ["#linux"], perform: [] },
    { id: "neo", name: "Neowin", host: "irc.neowin.net", port: 6697, tls: true, nick: "Andrew", authMethod: "nickserv", autoConnect: true, autoJoin: [], perform: [] },
  ],
  settings: { theme: "auto", lightTheme: "Light", darkTheme: "Dark", highlightWords: ["tshirtgang"], notifications: true, notifyPMs: true, inlineImages: true, inlineYouTube: true, inlineVideos: true, showJoinsParts: true, timeFormat: "24h", fontSize: 14, logging: true, historyLines: 200, quitMessage: "IRChatterbox", ignore: [] },
  nickColors: { caleo: "#f2b53c" },
  shortcuts: [{ trigger: "$shrug", text: "¯\\_(ツ)_/¯" }, { trigger: "$tableflip", text: "(╯°□°)╯︵ ┻━┻" }],
  plugins: { botPrefix: "[Bot] ", weatherEnabled: true, weatherFromMe: true, weatherFromOthers: true, weatherChannels: [], weatherCooldown: 5, urlCommands: [{ trigger: "!solar", url: "https://example.com/solar", template: "", maxLines: 3, cooldown: 30, enabled: true }], scriptsEnabled: true },
};
const users = ["~DaveLegg", "~Neobond", "@Andrew", "@Caleo", "%ammali", "%bemis|back", "%Enigma-Penguin|L", "%LogicalApex", "%The_Decryptor", "Alan-", "binaryzero", "brock_aholic", "Hackerpcs", "Kennyout", "kinghat", "Mike", "peachey|cloud", "Ryoken", "SierraSonic", "zyme"]
  .map((n) => ({ nick: n.replace(/^[~@%+]/, ""), prefix: /^[~@%+]/.test(n) ? n[0] : "" }));

const mockIcons = {};
export const api = {
  GetChannelIcons: () => ({ ...mockIcons }),
  SetChannelIcon: (server, buffer, url) => { if (url) mockIcons[server + "/" + buffer.toLowerCase()] = url; else delete mockIcons[server + "/" + buffer.toLowerCase()]; emit("icons", { ...mockIcons }); return ""; },
  GetConfig: () => { const c = structuredClone(cfg); c.settings.language = new URLSearchParams(location.search).get("lang") || ""; return c; },
  GetThemes: () => [],
  GetState: () => [
    { id: "lib", status: "connected", nick: "Andrew", network: "Libera.Chat", channels: [{ name: "#linux", topic: "Linux support · Be nice", users: users.slice(5, 12) }] },
    { id: "neo", status: "connected", nick: "Andrew", network: "Neowin", channels: [{ name: "#neowin", topic: "Welcome to #Neowin | Stats: \x02https://flawlesscowboy.xyz/ircstats\x02 | <Andrew> LTT screwdrivers, UNITE!", modes: "+cCfnstT", users }, { name: "#lounge", topic: "", users: users.slice(0, 6) }] },
  ],
  GetHistory: () => [],
  // stand-ins for the age key helpers: not real keys, just the right shape for the settings UI
  GenerateEncryptionKey: () => { const r = Array.from(crypto.getRandomValues(new Uint8Array(29)), (b) => "qpzry9x8gf2tvdw0s3jn54khce6mua7l"[b & 31]).join(""); return { privateKey: "AGE-SECRET-KEY-1" + r.toUpperCase().padEnd(58, "Q"), publicKey: "age1" + r.padEnd(58, "q") }; },
  EncryptionPublicKeys: (keys) => {
    const ks = keys.split("\n").map((l) => l.trim()).filter((l) => l && !l.startsWith("#"));
    if (ks.some((k) => !/^AGE-SECRET-KEY-1[0-9A-Z]{58}$/.test(k))) throw new Error("not an age private key");
    return ks.map((k) => "age1" + k.slice(16).toLowerCase());
  },
  PluginMenu: (ctx) => (ctx.kind === "nicklist" ? [{ id: "mirc:0", label: "Slap" }, { label: "Fun", items: [{ id: "js:fun.js:0", label: "Wave" }, { id: "js:fun.js:1", label: "Hug" }] }] : []),
  RunPluginMenu: (ctx, id) => console.log("menu", id, ctx.nick),
  GetDiagnostics: () => (new URLSearchParams(location.search).has("crash") ? { lastCrash: "crash-2026-10-17.log", version: "0.4.0-dev", os: "windows/amd64" } : { lastCrash: "" }),
  SetNickColor: (n, c) => { if (c) cfg.nickColors[n.toLowerCase()] = c; else delete cfg.nickColors[n.toLowerCase()]; setTimeout(() => emit("config", structuredClone(cfg))); },
  Quit: () => console.log("QUIT"),
  CancelClose: () => console.log("CANCEL"),
  TestMusic: () => ["Windows media controls: ✓ Daft Punk - One More Time  (MusicBee)", "MusicBee window title: ✓ Daft Punk - One More Time  (MusicBee)", "Text file: not set up", "", "Reply: [Bot] Now playing: Daft Punk - One More Time"],
  PickFile: () => "C:\\Users\\Andrew\\Documents\\nowplaying.txt",
  SetAutoJoin: (sid, ch, on) => { const sv = cfg.servers.find((x) => x.id === sid); sv.autoJoin = on ? [...sv.autoJoin, ch] : sv.autoJoin.filter((a) => a.toLowerCase() !== ch.toLowerCase()); },
  SetStartView: (s, b) => { cfg.startView = { server: s, buffer: b }; },
  ToggleFullscreen: () => {},
  PickFolder: () => "D:\\IRC Logs",
  OpenLogFolder: () => {},
  GetVersion: () => "0.2.5",
  GetEncodings: () => [["auto", "Automatic (UTF-8, fall back to Western)"], ["utf-8", "UTF-8 only"], ["windows-1251", "Cyrillic (Windows-1251)"]],
  PluginCommands: () => ["slap", "count"],
  CheckForUpdate: () => ({ available: true, current: "0.2.5", latest: "0.3.0", url: "https://github.com/phpandrew/IRChatterbox/releases" }),
  InstallUpdate: () => new Promise((r) => { let p = 0; const t = setInterval(() => { p += 20; emit("updateProgress", { pct: p }); if (p >= 100) { clearInterval(t); r(""); } }, 200); }),
  SkipUpdateVersion: () => {},
  ReorderServers: (ids) => console.log("order", ids.join(",")),
  SetChannelOrder: (s, o) => console.log("chorder", s, o.join(",")),
  PickSoundFile: () => "C:\\Sounds\\airhorn.mp3",
  GetSoundData: () => "",
  GetDefaultNick: () => "Andrew",
  SaveServer: (sv) => ({ ...sv, id: sv.id || "new1" }),
  Connect: () => {},
  SaveSettings: (st) => { cfg.settings = st; return ""; },
  UploadImage: () => new Promise((r) => setTimeout(() => r({ url: "https://i.irchatterbox.com/k3x9Qa2mZp.webp", expires: "2026-10-24T18:00:00Z", host: "irchatterbox" }), 800)),
  SaveTheme: () => null,
  SaveShortcuts: (l) => { cfg.shortcuts = l; },
  SendInput: (s, b, t) => { if (!t.startsWith("/")) emit("irc", { type: "msg", server: s, buffer: b, kind: "privmsg", nick: "Andrew", prefix: "@", text: t, self: true, time: Date.now(), extra: { msgid: "m" + Math.random().toString(36).slice(2) } }); },
  SendReply: (s, b, id, t) => emit("irc", { type: "msg", server: s, buffer: b, kind: "privmsg", nick: "Andrew", prefix: "@", text: t, self: true, time: Date.now(), extra: { msgid: "m" + Math.random().toString(36).slice(2), reply: id } }),
  React: (s, b, id, e, remove) => { emit("irc", { type: "react", server: s, buffer: b, nick: "Andrew", text: e, self: true, time: Date.now(), extra: { msgid: id, remove } }); return ""; },
  GetLinkPreview: (url) => new Promise((r) => setTimeout(() => r(new URL(url).hostname === "github.com"
    ? { url, title: "phpandrew/IRChatterbox: A modern IRC client for Windows, macOS, Linux and Android", description: "Inline images, coloured nicks, plugins, mIRC scripts, bouncer history and 11 languages. Built with Go and Wails.", image: "https://picsum.photos/id/180/300/300", site: "GitHub" }
    : { url, error: "no preview" }), 300)),
  SetFavourite: (s, b, on) => {
    cfg.favourites = (cfg.favourites || []).filter((f) => !(f.server === s && f.buffer.toLowerCase() === b.toLowerCase()));
    if (on) cfg.favourites.push({ server: s, buffer: b });
    setTimeout(() => emit("config", structuredClone(cfg)));
  },
  SetFavouriteOrder: (list) => { cfg.favourites = list; },
  SetChannelNotify: (s, b, level, minutes) => {
    cfg.channelNotify ||= {};
    const k = s + "/" + b.toLowerCase();
    if (!level) delete cfg.channelNotify[k]; else cfg.channelNotify[k] = { level, until: level === "mute" && minutes ? Date.now() + minutes * 60e3 : 0 };
    setTimeout(() => emit("config", structuredClone(cfg)));
  },
  DCCAccept: (id) => { dccDemo(id); return ""; },
  DCCDecline: (id) => { const t = mockDCC[id]; if (t) { t.state = t.state === "offer" ? "declined" : "cancelled"; emit("dcc", { ...t }); } },
  DCCSend: (s, nick, path) => { const t = { id: "o" + Date.now(), server: s, nick, kind: "send", dir: "out", file: path.split(/[\\/]/).pop(), size: 2400000, state: "waiting", time: Date.now() }; mockDCC[t.id] = t; emit("dcc", { ...t }); return ""; },
  DCCChat: () => "",
  ShowDCCFile: () => {},
  PickDCCFolder: () => "D:\\Downloads\\IRC",
  AutoConnect: () => {
    const base = Date.now() - 3600e3;
    const lines = [
      ["Alan-", "", "why not a c core and modular so you can make native frontends per OS"],
      ["Andrew", "@", "why not Go?"],
      ["Enigma-Penguin|L", "%", "Amatures, why not Haskell"],
      ["Andrew", "@", "go has easy to build compiler"],
      ["Alan-", "", "go just works and can cross compile for like 95% of systems"],
      ["Andrew", "@", "i asked for a mockup of an irc client https://picsum.photos/id/1018/800/500.jpg"],
      ["Alan-", "", "hmm it thinks irc is discord"],
      ["Caleo", "@", "I mean, isn't discord just irc with extra steps and voice chat?"],
      ["Kennyout", "", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"],
      ["Andrew", "@", "Today it feels like 20c"],
      ["Andrew", "@", "[Bot] 20 C = 68 F"],
      ["Laura", "%", "\x0304colours\x03 and \x02bold\x02 work too, \x1ditalic\x1d"],
      ["Alan-", "", "alcohol in a plastic ball, Andrew", true],
    ];
    lines.forEach(([n, p, t, hl], i) => emit("irc", { type: "msg", server: "neo", buffer: "#neowin", kind: "privmsg", nick: n, prefix: p, text: t, self: n === "Andrew", highlight: !!hl, time: base + i * 60e3 }));
    emit("irc", { type: "event", kind: "join", server: "neo", buffer: "#neowin", nick: "sam", text: "sam@user/sam", time: base + 14 * 60e3 });
    emit("irc", { type: "msg", server: "neo", buffer: "#lounge", kind: "privmsg", nick: "Caleo", text: "anyone around?", time: Date.now() });
    emit("irc", { type: "msg", server: "lib", buffer: "#linux", kind: "privmsg", nick: "flux", text: "andrew: did tshirtgang move to the new box?", highlight: true, time: Date.now() });
    emit("irc", { type: "info", server: "neo", buffer: "", kind: "motd", text: "- Welcome to the Neowin IRC network", time: Date.now() });
    // replies, reactions and a link preview
    const t0 = base + 15 * 60e3;
    emit("irc", { type: "msg", server: "neo", buffer: "#neowin", kind: "privmsg", nick: "Caleo", prefix: "@", text: "anyone tried the new client build yet? https://github.com/phpandrew/IRChatterbox", time: t0, extra: { msgid: "q1" } });
    emit("irc", { type: "msg", server: "neo", buffer: "#neowin", kind: "privmsg", nick: "Alan-", text: "yes, the reactions are slick", time: t0 + 60e3, extra: { msgid: "q2", reply: "q1" } });
    for (const [n, e] of [["Alan-", "👍"], ["Kennyout", "👍"], ["Andrew", "👍"], ["zyme", "🔥"]]) emit("irc", { type: "react", server: "neo", buffer: "#neowin", nick: n, text: e, self: n === "Andrew", time: t0 + 70e3, extra: { msgid: "q1" } });
    emit("irc", { type: "react", server: "neo", buffer: "#neowin", nick: "Caleo", text: "😂", time: t0 + 80e3, extra: { msgid: "q2" } });
    if (new URLSearchParams(location.search).has("dcc")) {
      const t = { id: "d1", server: "neo", nick: "Kennyout", kind: "send", dir: "in", file: "holiday photos.zip", size: 48234000, addr: "203.0.113.7:5001", state: "offer", time: Date.now() };
      mockDCC.d1 = t;
      setTimeout(() => emit("dcc", { ...t }), 50);
    }
  },
};
const mockDCC = {};
function dccDemo(id) {
  const t = mockDCC[id];
  if (!t) return;
  t.state = "active"; t.done = 0; emit("dcc", { ...t });
  const step = setInterval(() => {
    t.done = Math.min(t.size, t.done + t.size / 8);
    if (t.done >= t.size) { t.state = "done"; clearInterval(step); }
    emit("dcc", { ...t });
  }, 250);
}
export function install() { window.__mockEmit = emit; }
