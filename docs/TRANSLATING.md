# Translating IRChatterbox

IRChatterbox's menus and settings come in English, French, Spanish, German, Portuguese (Brazil), Italian, Russian, Polish, Turkish, Simplified Chinese and Japanese. The language follows the system, or can be picked in **Settings → Appearance → Language**. Chat messages, nicks, channel names and topics are never translated.

## How it works

The interface is written in English. `source/frontend/src/i18n.js` watches the page and swaps each piece of English text for its translation as soon as it's drawn: text, and the `title`, `placeholder`, `aria-label` and `alt` attributes. The translations are in `source/frontend/src/locales/<code>.json`:

```json
{
  "Leave channel": "Quitter le salon",
  "Users — {0}": "Utilisateurs — {0}",
  "{0} is typing…": "{0} est en train d'écrire…"
}
```

- **The key is the exact English text** as it appears on screen, without leading or trailing spaces.
- **`{0}`, `{1}`…** stand for parts that change, such as a nick, a channel or a number. Keep each one in the translation; you can move them around to fit the grammar.
- **Missing strings stay in English,** so a partial translation still works.

## Improving a translation

Edit the language's JSON file and open a pull request. The wording was machine-assisted, so corrections from native speakers are very welcome.

## Adding a language

1. Copy `locales/fr.json` to `locales/<code>.json` (e.g. `nl.json`) and translate the values.
2. Add the language to `LANGUAGES` at the top of `i18n.js`, with its name written in that language: `["nl", "Nederlands"]`.
3. Add the code to the `language` comment in `source/core/config.go`.

## When you add English text to the app

New strings show in English until someone adds them to the locale files. Text that's built from pieces, such as `` `${n} users` ``, needs a key with a placeholder: `"{0} users"`.

Messages that come from the Go side, such as connection errors in the server window, aren't translated yet.
