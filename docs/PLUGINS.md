# Writing IRChatterbox plugins

Plugins are small JavaScript files that react to what happens on IRC: messages, joins, nick changes and so on. They can reply, add their own `/commands`, run on timers, fetch web pages and remember data between restarts.

## Quick start

1. Open **Settings → Plugins → Open data folder**, then go into the `plugins` folder.
   (It's `%APPDATA%\IRChatterbox\plugins`.)
2. Create a file called `hello.js`:

   ```js
   on("message", function (e) {
     if (e.text === "!hello") {
       reply(prefix + "Hello, " + e.nick + "!");
     }
   });
   ```

3. Type `/reload` in IRChatterbox. The server window shows which plugins loaded and any errors.
4. Someone types `!hello`, and you reply `[Bot] Hello, bob!`.

Every `.js` file in the folder is loaded at startup and on `/reload`. Files whose names start with `_` are skipped; `_example.js` is a ready-made example to copy. Plugins can be switched off entirely in **Settings → Plugins → Script plugins**.

Coming from mIRC? `.mrc` scripts go in the same folder and run alongside these. See [mIRC scripts](MIRC-SCRIPTS.md).

## How plugins run

- Each plugin runs in its own sandbox. Plugins can't see each other's variables, read files or start programs.
- A plugin's handlers run **one at a time, in order**, so you never need locks.
- A handler that runs longer than **15 seconds** is stopped. Errors and crashes are printed in the server window as `[yourplugin.js] …`, and the plugin keeps running.
- The language is modern JavaScript (ES2015+, via [goja](https://github.com/dop251/goja)): arrow functions, `let`/`const`, template strings, classes and regex all work. There's no `require`, no `fetch` and no Node APIs; use the helpers listed below.

---

## Events

```js
on("eventName", function (e) { ... });
```

Every event object has these fields:

| Field | Meaning |
|---|---|
| `e.server` | Network name, as set in the server editor (e.g. `Libera.Chat`) |
| `e.serverId` | Internal id of that server |
| `e.me` | Your current nick on that network |
| `e.target` | Where `reply()` sends to (the channel, or the person in a private chat) |

| Event | When | Extra fields |
|---|---|---|
| `message` | Someone, including you, sends a normal message | `channel`, `nick`, `text` (formatting removed), `raw` (with colour codes), `self`, `isChannel`, `isPM` |
| `action` | `/me` actions from others | same as `message` |
| `notice` | A NOTICE from a user | same as `message` |
| `join` | Someone joins a channel (`self` = you) | `channel`, `nick`, `self` |
| `part` | Someone leaves a channel | `channel`, `nick`, `reason`, `self` |
| `quit` | Someone disconnects | `nick`, `reason`, `channels` (the channels you shared) |
| `kick` | Someone is kicked | `channel`, `nick` (who kicked), `victim`, `reason`, `self` (you were kicked) |
| `nick` | Someone changes nick | `oldNick`, `newNick`, `self`, `channels` |
| `topic` | A topic is changed | `channel`, `nick`, `topic`, `self` |
| `mode` | Channel modes change | `channel`, `nick`, `modes` (e.g. `+o bob`) |
| `invite` | Someone invites you | `channel`, `nick`, `invited` |
| `connect` | You finish connecting to a network | `nick` |
| `disconnect` | You're disconnected | `reason` |
| `raw` | **Every** line from the server (advanced) | `command` (e.g. `PRIVMSG`, `001`), `params` (array), `nick`, `prefix`, `tags` |
| `load` | The plugin has just been loaded or reloaded | — |

Your own messages arrive as `message` events with `e.self === true`. Check it so your bot doesn't answer itself:

```js
on("message", (e) => {
  if (e.self) return;
  // ...
});
```

## Commands

Add your own slash commands. They also appear in the `/` autocomplete list.

```js
command("slap", function (e) {
  // typed: /slap bob with a trout
  // e.args = "bob with a trout", e.argv = ["bob", "with", "a", "trout"]
  if (!e.argv.length) return log("usage: /slap nick");
  action(e.channel, "slaps " + e.argv[0] + " around a bit with a large trout");
});
```

The command event has `command`, `args`, `argv`, `channel` (the window you typed in), `server`, `me` and `isChannel`.
IRChatterbox's built-in commands (such as `/join` and `/kick`) take priority over plugin commands with the same name.

## Right-click menus

Add items to IRChatterbox's right-click menus. They appear at the bottom, after a separator.

```js
menu("nicklist", "Slap", function (e) {
  action(e.channel, "slaps " + e.nick + " around a bit with a large trout");
});
menu("nicklist,query", "Fun/Hug", function (e) {   // "Fun/Hug" → a Fun submenu
  action(e.channel || e.nick, "hugs " + e.nick);
});
menu("message", "Quote", function (e) {
  reply("> " + e.nick + ": " + e.text);
});
```

| Menu | Opens when you right-click | Event has |
|---|---|---|
| `nicklist` | a nick, in the user list or the chat | `nick`, `channel` |
| `channel` | a channel in the sidebar | `channel` |
| `query` | a private chat in the sidebar | `nick`, `channel` (the query) |
| `status` | a server in the sidebar | — |
| `message` | a message in the chat | `nick`, `text`, `channel` |

Every event also has `menu`, `server`, `me` and `isChannel`. `reply()` answers in the window the menu was opened from. Use several menus at once with commas: `menu("nicklist,query", …)`.

## Sending

| Function | What it does |
|---|---|
| `reply(text)` | Send to wherever the event happened (channel or private chat) |
| `say(target, text)` | Send a message to a channel or nick |
| `action(target, text)` | Send a `/me` action |
| `notice(target, text)` | Send a NOTICE |
| `run("/command args")` | Run any command as if you typed it, e.g. `run("/join #test")` |
| `raw("IRC LINE")` | Send a raw IRC line, e.g. `raw("MODE #chan +m")` |

Long messages are split automatically, and everything is flood-protected the same way your own typing is.
Start bot replies with `prefix` (your Bot reply prefix, `[Bot] ` by default). IRChatterbox's built-in features never respond to lines starting with it, which stops two bots answering each other forever.

## Information

| Function | Returns |
|---|---|
| `me()` | Your current nick |
| `channels()` | The channels you're in on this network, e.g. `["#linux", "#neowin"]` |
| `users("#chan")` | `[{ nick, prefix, away }, …]`, where `prefix` is `"@"`, `"+"`, `"@+"`, … |
| `isOp("#chan", nick)` | `true` if that nick is a channel operator (or higher) |
| `stripFormatting(text)` | Text without IRC colour and bold codes |
| `prefix` | Your bot reply prefix from Settings (a string, not a function) |

## Talking to you

| Function | What it does |
|---|---|
| `log(text)` | Print `[plugin.js] text` in the server window. Only you see it. |
| `notify(title, body)` | Show a Windows notification |

## Timers

```js
const id = setTimeout(() => reply("5 seconds later"), 5000);
clearTimeout(id);

// every 10 minutes (intervals run at most once a second)
const t = setInterval(() => say("#mychan", "Remember to hydrate 💧"), 10 * 60 * 1000);
clearInterval(t);
```

A timer remembers where it was created, so `reply()` inside it goes to the same channel. All timers are cancelled when the plugin is reloaded.

## Storage that survives restarts

```js
store.set("count", 5);
store.get("count", 0);   // → 5  (second argument = default if missing)
store.delete("count");
store.keys();            // → ["…", …]
```

Values can be numbers, strings, booleans, arrays or plain objects. Each plugin has its own file, `plugins\data\<plugin name>.json`, which you can open and edit.

## Web requests

```js
const html = httpGet("https://example.com");                          // body as a string ("" on error)
const data = httpGetJSON("https://api.github.com/repos/phpandrew/IRChatterbox"); // parsed JSON, or null
if (data) reply(prefix + "⭐ " + data.stargazers_count + " stars");
```

Requests time out after 12 seconds and responses are capped (512 KB for text, 2 MB for JSON). For simple "type `!x`, fetch a URL, post the result" commands, **Settings → Plugins → URL commands** does the job without writing any code.

## Helpers

`toF(celsius)` and `toC(fahrenheit)` convert temperatures.

---

## Examples

### Greeter

```js
on("join", (e) => {
  if (!e.self && e.channel === "#mychan") {
    say(e.channel, `${prefix}Welcome to ${e.channel}, ${e.nick}! 👋`);
  }
});
```

### Karma: `nick++` and `nick--`

```js
on("message", (e) => {
  const m = e.text.match(/^(\S+)(\+\+|--)$/);
  if (!m || m[1].toLowerCase() === e.nick.toLowerCase()) return; // no self-karma
  const key = "karma:" + m[1].toLowerCase();
  const n = store.get(key, 0) + (m[2] === "++" ? 1 : -1);
  store.set(key, n);
  reply(`${prefix}${m[1]} now has ${n} karma`);
});

command("karma", (e) => {
  const who = (e.argv[0] || me()).toLowerCase();
  log(`${who}: ${store.get("karma:" + who, 0)} karma`);
});
```

### Seen

```js
on("message", (e) => {
  store.set("seen:" + e.nick.toLowerCase(), { when: Date.now(), where: e.channel, text: e.text });
  const m = e.text.match(/^!seen (\S+)/);
  if (!m) return;
  const s = store.get("seen:" + m[1].toLowerCase(), null);
  if (!s) return reply(`${prefix}I haven't seen ${m[1]}.`);
  const mins = Math.round((Date.now() - s.when) / 60000);
  reply(`${prefix}${m[1]} was last seen ${mins} min ago in ${s.where}: "${s.text}"`);
});
```

### Auto-op your friends

```js
const friends = ["alice", "bob"];
on("join", (e) => {
  if (!e.self && friends.includes(e.nick.toLowerCase()) && isOp(e.channel, me())) {
    run(`/op ${e.nick}`);
  }
});
```

### Rejoin when kicked

```js
on("kick", (e) => {
  if (e.self) setTimeout(() => run("/join " + e.channel), 3000);
});
```

### React to a numeric (raw)

```js
on("raw", (e) => {
  if (e.command === "001") log("Registered on " + e.server); // RPL_WELCOME
});
```

## Troubleshooting

- **Nothing happens.** Run `/reload` and look at the server window. A syntax error shows the file name and line.
- **"unknown event".** Check the spelling against the events table above.
- **The bot answers itself.** Return early when `e.self` is true, or begin your replies with `prefix`.
- **"event queue full".** A handler is blocking, usually a slow `httpGet` inside a busy `message` handler. Only fetch when a trigger matches.
