// Browser-only mock backend used for UI development (not included in normal use).
const listeners = {};
export function on(ev, fn) { (listeners[ev] ||= []).push(fn); }
function emit(ev, data) { (listeners[ev] || []).forEach((f) => f(data)); }

const cfg = {
  servers: [
    { id: "lib", name: "Libera.Chat", host: "irc.libera.chat", port: 6697, tls: true, nick: "Andrew", authMethod: "sasl", autoConnect: true, autoJoin: ["#linux"], perform: [] },
    { id: "neo", name: "Neowin", host: "irc.neowin.net", port: 6697, tls: true, nick: "Andrew", authMethod: "nickserv", autoConnect: true, autoJoin: [], perform: [] },
  ],
  settings: { theme: "auto", lightTheme: "Light", darkTheme: "Dark", highlightWords: ["tshirtgang"], notifications: true, notifyPMs: true, inlineImages: true, inlineYouTube: true, showJoinsParts: true, timeFormat: "24h", fontSize: 14, logging: true, historyLines: 200, quitMessage: "IRChatterbox", ignore: [] },
  nickColors: { caleo: "#f2b53c" },
  shortcuts: [{ trigger: "$whatever", text: "¯\\_(ツ)_/¯" }, { trigger: "$shrug", text: "¯\\_(ツ)_/¯" }, { trigger: "$tableflip", text: "(╯°□°)╯︵ ┻━┻" }],
  plugins: { botPrefix: "[Bot] ", weatherEnabled: true, weatherFromMe: true, weatherFromOthers: true, weatherChannels: [], weatherCooldown: 5, urlCommands: [{ trigger: "!solar", url: "https://example.com/solar", template: "", maxLines: 3, cooldown: 30, enabled: true }], scriptsEnabled: true },
};
const users = ["~DaveLegg", "~Neobond", "@Andrew", "@Caleo", "%ammali", "%bemis|back", "%Enigma-Penguin|L", "%LogicalApex", "%The_Decryptor", "Alan-", "binaryzero", "brock_aholic", "Hackerpcs", "Kennyout", "kinghat", "Mike", "peachey|cloud", "Ryoken", "SierraSonic", "zyme"]
  .map((n) => ({ nick: n.replace(/^[~@%+]/, ""), prefix: /^[~@%+]/.test(n) ? n[0] : "" }));

export const api = {
  GetConfig: () => structuredClone(cfg),
  GetThemes: () => [],
  GetState: () => [
    { id: "lib", status: "connected", nick: "Andrew", network: "Libera.Chat", channels: [{ name: "#linux", topic: "Linux support · Be nice", users: users.slice(5, 12) }] },
    { id: "neo", status: "connected", nick: "Andrew", network: "Neowin", channels: [{ name: "#neowin", topic: "Welcome to #Neowin | Stats: \x02https://flawlesscowboy.xyz/ircstats\x02 | <Andrew> LTT screwdrivers, UNITE!", modes: "+cCfnstT", users }, { name: "#lounge", topic: "", users: users.slice(0, 6) }] },
  ],
  GetHistory: () => [],
  SaveShortcuts: (l) => { cfg.shortcuts = l; },
  SendInput: (s, b, t) => { if (!t.startsWith("/")) emit("irc", { type: "msg", server: s, buffer: b, kind: "privmsg", nick: "Andrew", prefix: "@", text: t, self: true, time: Date.now() }); },
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
      ["Andrew", "@", "[Bot] 20c = 68F"],
      ["Laura", "%", "\x0304colours\x03 and \x02bold\x02 work too, \x1ditalic\x1d"],
      ["Alan-", "", "alcohol in a plastic ball, Andrew", true],
    ];
    lines.forEach(([n, p, t, hl], i) => emit("irc", { type: "msg", server: "neo", buffer: "#neowin", kind: "privmsg", nick: n, prefix: p, text: t, self: n === "Andrew", highlight: !!hl, time: base + i * 60e3 }));
    emit("irc", { type: "event", kind: "join", server: "neo", buffer: "#neowin", nick: "sam", text: "sam@user/sam", time: base + 14 * 60e3 });
    emit("irc", { type: "msg", server: "neo", buffer: "#lounge", kind: "privmsg", nick: "Caleo", text: "anyone around?", time: Date.now() });
    emit("irc", { type: "msg", server: "lib", buffer: "#linux", kind: "privmsg", nick: "flux", text: "andrew: did tshirtgang move to the new box?", highlight: true, time: Date.now() });
    emit("irc", { type: "info", server: "neo", buffer: "", kind: "motd", text: "- Welcome to the Neowin IRC network", time: Date.now() });
  },
};
export function install() {}
