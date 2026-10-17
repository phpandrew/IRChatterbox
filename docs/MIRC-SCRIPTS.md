# mIRC scripts

IRChatterbox runs mIRC scripts (`.mrc` files). It isn't a full mIRC: it covers the parts most shared scripts use (aliases, events, variables, conditions, loops, hash tables, timers and the common identifiers), and skips anything that touches files, programs or windows.

## Quick start

1. Open **Settings → Plugins → Open data folder**, then go into the `plugins` folder.
2. Copy your `.mrc` file there. (`_example.mrc` is a starting point; files starting with `_` aren't loaded.)
3. Type `/reload`. The server window lists what loaded and anything that was skipped.

`.mrc` scripts and [JavaScript plugins](PLUGINS.md) live in the same folder and run side by side. Both are switched on and off with **Settings → Plugins → Script plugins**.

```
alias hello {
  if ($1 == $null) { echo -a Usage: /hello nick | return }
  msg $chan Hello $1 $+ , from $me!
}

on *:TEXT:!dice:#:msg $chan $nick rolled a $rand(1,6)

on !*:JOIN:#:notice $nick Welcome to $chan $+ , $nick $+ !
```

## What's supported

**Definitions**

| | |
|---|---|
| `alias name { … }` | Becomes `/name`, and `$name(args)` returns its `/return` value. `alias -l` works too. |
| `on <level>:<EVENT>:…:{ … }` | Events below. Levels `*`, `1`, `me:`, `!` (not for you), `@` (only when you're an op), `$` (regex match) |
| `raw <numeric>:<match>:{ … }` | Server numerics, e.g. `raw 332:*:` or `raw *:*:` |
| `menu nicklist,channel,query,status { … }` | Right-click menu items (below) |
| `#group on` … `#group end` | Accepted (group lines are ignored) |
| `;` comments, `/* … */` blocks | Ignored |

**Events:** TEXT, ACTION, NOTICE (match text and `#`, `?`, `*` or a channel list), JOIN, PART, KICK, TOPIC, INVITE, RAWMODE, INPUT (channel target), QUIT, NICK, CONNECT, DISCONNECT, START, LOAD.

On TEXT/ACTION/NOTICE run for other people's messages, as in mIRC. Matches use mIRC wildcards (`*`, `?`, `&`); with the `$` level the match is a regex like `/^!w(eather)?\s/i`. History replayed from a bouncer never triggers events.

**on INPUT** runs on what you type. `/halt` or `/haltdef` stops the line being sent, so a script can rewrite it:

```
on *:INPUT:#:{ if ($1 == !shout) { say $upper($2-) | halt } }
```

**Commands**

- **Messages:** `/msg`, `/say`, `/notice`, `/describe`, `/me`, `/echo [-flags] [#chan] text` (shown only to you)
- **Variables:** `/set`, `/unset` (wildcards like `%seen.*`), `/inc`, `/dec`, `/var %a = 1, %b`, `%x = value` (simple maths like `%x = %x + 1` works)
- **Flow:** `if` / `elseif` / `else`, `while`, `break`, `continue`, `return`, `halt`, `haltdef`
- **Hash tables:** `/hmake`, `/hadd`, `/hinc`, `/hdec`, `/hdel`, `/hfree`, and `$hget(table, item)`
- **Timers:** `/timer[name] [-m] reps delay command`, `/timer<name> off`, `/timers off`. As in mIRC the command is evaluated when it's set and again when it fires; write `$!nick` to leave an identifier for later.
- **Everything else** goes to IRChatterbox's own commands, so `/join`, `/part`, `/kick`, `/mode`, `/op`, `/topic`, `/away`, `/raw` and the rest work as usual.
- `|` separates commands; `[ ]` evaluation brackets work (`%count. [ $+ [ $nick ] ]`).

**Identifiers**

- **Event:** `$nick`, `$chan`, `$1`, `$2-`, `$1-3`, `$0`, `$me`, `$address`, `$fulladdress`, `$site`, `$knick`, `$newnick`, `$target`, `$network`, `$server`, `$port`
- **Text:** `$len`, `$upper`, `$lower`, `$left`, `$right`, `$mid`, `$pos`, `$count`, `$replace`, `$replacecs`, `$remove`, `$str`, `$strip`, `$chr`, `$asc`, `$+`, `$+(a,b,c)`
- **Tokens:** `$gettok`, `$numtok`, `$addtok`, `$deltok`, `$remtok`, `$istok`, `$findtok`
- **Maths:** `$calc`, `$int`, `$abs`, `$round`, `$rand`
- **Time:** `$time`, `$time(format)`, `$date`, `$ctime`, `$ticks`, `$asctime(ctime, format)`, `$duration`
- **People and channels:** `$nick(#chan, N)`, `$nick(#chan, 0)`, `$comchan`, `$address(nick, type)`, `$mask(address, type)`
- **Other:** `$iif`, `$var`, `$null`, `$true`, `$false`, `$version`

**Operators in `if` and `while`:** `==`, `===`, `!=`, `<`, `>`, `<=`, `>=`, `//`, `\\`, `isin`, `isincs`, `iswm`, `iswmcs`, `isnum`, `isalpha`, `isalnum`, `isletter`, `islower`, `isupper`, `ison`, `isop`, `ishop`, `isvoice`, `isreg`, `ischan`; put `!` in front to negate (`!isin`); combine with `&&`, `||` and `!( … )`.

## Right-click menus

`menu` blocks add items to the right-click menus, after a separator:

```
menu nicklist {
  Whois:whois $$1
  -
  Fun
  .Slap:me slaps $$1 around a bit with a large trout
  .Hug:{
    me hugs $$1
    inc %hugs
  }
  $iif($me isop $chan, Op tools)
  .Kick:kick $chan $$1
}
```

- `nicklist` opens on a nick (user list or chat; `$1` and `$snicks` are the nick), `channel` on a channel in the sidebar, `query` on a private chat (`$1` is the nick), `status` on a server. `menu *` adds to all four.
- `.` makes a submenu item, `..` one level deeper. `-` is a separator.
- Labels are evaluated when the menu opens; an item whose label comes out empty is hidden (as with `$iif` above).
- `$$1` stops the command if there's no value.
- `&` in a label is dropped (mIRC uses it for keyboard shortcuts).

## Not supported

These are skipped when a script loads, or do nothing when called:

- Dialogs, custom windows, `menubar` and `@window` menus, `$submenu`, `ctcp` events, picture windows
- Files and INI: `/write`, `$read`, `/writeini`, `$readini`, `/fopen`, `$findfile`
- Programs and DLLs: `/run`, `/dll`, `$com`
- Sockets: `/sockopen` and friends (JavaScript plugins have `httpGet` for web requests)
- `/load`, `/unload`, `/reload` and `/exit` from inside a script

A script that relies on these mostly still loads; just those parts don't run. For anything bigger, a [JavaScript plugin](PLUGINS.md) can do more.

## Good to know

- **Global variables** (`%name`) are saved to `plugins/data/mirc-vars.json` and survive restarts. Hash tables last until IRChatterbox closes.
- **One at a time:** scripts run in order, one event after another, like mIRC.
- **Runaway loops stop:** a script that runs about 200,000 steps (an endless `while`) is stopped, with a message in the server window. Errors show there as `[script.mrc] …`.
- **No double evaluation of text:** what someone says is never run as script code, except where you ask for it with a `/timer` whose command includes their text, as in mIRC.
