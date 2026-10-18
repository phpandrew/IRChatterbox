// Interface languages. The UI is written in English; this module swaps the visible text for
// the chosen language as soon as it's drawn: text, and the title / placeholder / aria-label /
// alt attributes. Dictionaries live in ./locales/<code>.json and map the English text to the
// translation; "{0}" in a key matches any text (a nick, a number) and is carried over.
//
// Chat messages, nicks, channel names and topics are never translated.

const loaders = import.meta.glob("./locales/*.json");

export const LANGUAGES = [
  ["en", "English"],
  ["fr", "Français"],
  ["es", "Español"],
  ["de", "Deutsch"],
  ["pt-BR", "Português (Brasil)"],
  ["it", "Italiano"],
  ["ru", "Русский"],
  ["pl", "Polski"],
  ["tr", "Türkçe"],
  ["zh-CN", "简体中文"],
  ["ja", "日本語"],
];

// Where text is someone's content rather than the app's: left alone.
const SKIP = [
  ".line:not(.ev) .msg", ".line .who", ".line .nick", ".h-topic", "#hName", ".ul-user", "#tree .buf:not([data-k=\"\"]) .nm", ".srv-name",
  "#meNick", "code", "kbd", "pre", "textarea", ".paste-preview", ".report-msg", ".test-out", "#emojiPicker",
  ".no-i18n", ".nickref", ".media", "[contenteditable]",
].join(",");
const ATTRS = ["title", "placeholder", "aria-label", "alt"];

let current = "en";
let exact = null; // English → translation
let patterns = []; // [regex, translation]
const origText = new WeakMap(); // text node → { en, shown }
const origAttr = new WeakMap(); // element → { attr: { en, shown } }
let observer = null;

// Pick the best match for the system's languages ("pt-PT" → pt-BR, "zh-Hans-CN" → zh-CN).
export function systemLanguage() {
  const codes = LANGUAGES.map(([c]) => c);
  for (const raw of navigator.languages || [navigator.language || "en"]) {
    const l = String(raw);
    const hit = codes.find((c) => c.toLowerCase() === l.toLowerCase());
    if (hit) return hit;
    const base = l.split("-")[0].toLowerCase();
    if (base === "zh") return "zh-CN";
    if (base === "pt") return "pt-BR";
    const b = codes.find((c) => c.split("-")[0] === base);
    if (b) return b;
  }
  return "en";
}

export function currentLanguage() {
  return current;
}

function compile(dict) {
  exact = new Map();
  patterns = [];
  for (const [en, tr] of Object.entries(dict)) {
    if (typeof tr !== "string" || !tr) continue;
    if (/\{\d\}/.test(en)) {
      const order = [];
      const parts = en.split(/(\{\d\})/);
      const src = parts.map((part, i) => {
        const m = /^\{(\d)\}$/.exec(part);
        if (!m) return part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        order.push(+m[1]);
        // glued to a word ("day{1}"): an English plural ending, which may be empty
        return /[A-Za-z]$/.test(parts[i - 1] || "") ? "([a-z]{0,2})" : "(.+?)";
      }).join("");
      const literal = en.replace(/\{\d\}/g, "").length;
      patterns.push({ re: new RegExp("^" + src + "$", "s"), order, tr, literal });
    } else {
      exact.set(en, tr);
    }
  }
  patterns.sort((a, b) => b.literal - a.literal); // most specific first
}

// translate returns the translation of an English string, or null.
function translate(s) {
  if (!exact) return null;
  const hit = exact.get(s);
  if (hit !== undefined) return hit;
  for (const p of patterns) {
    if (p.literal < 2) continue;
    const m = p.re.exec(s);
    // a short pattern ("{0} match{1}") must not swallow a whole sentence
    if (m && m.slice(1).some((g) => g.length > 80 || (p.literal < 10 && g.length > 30))) continue;
    if (m) return p.tr.replace(/\{(\d)\}/g, (_, i) => { const at = p.order.indexOf(+i); return at >= 0 ? m[at + 1] : ""; });
  }
  return null;
}

// t() for code that needs a translated string directly (e.g. confirm text).
export function t(en, ...args) {
  const s = args.length ? en.replace(/\{(\d)\}/g, (_, i) => args[+i] ?? "") : en;
  const keyed = translate(s);
  return keyed ?? s;
}

function skipped(el) {
  return !el || el.closest?.(SKIP);
}

function doText(node) {
  const value = node.nodeValue;
  if (!value || !/[A-Za-z]/.test(value) && !origText.has(node)) return;
  const rec = origText.get(node);
  // the app changed the text since we translated it: start again from the new English
  const en = rec && rec.shown === value ? rec.en : value;
  const core = en.trim();
  if (!core) return;
  const tr = current === "en" ? null : translate(core);
  const out = tr == null ? en : en.replace(core, tr);
  origText.set(node, { en, shown: out });
  if (out !== value) node.nodeValue = out;
}

function doAttrs(el) {
  let rec = origAttr.get(el);
  for (const a of ATTRS) {
    const value = el.getAttribute(a);
    if (value == null || !value.trim()) continue;
    const prev = rec?.[a];
    const en = prev && prev.shown === value ? prev.en : value;
    const tr = current === "en" ? null : translate(en.trim());
    const out = tr == null ? en : tr;
    if (!rec) { rec = {}; origAttr.set(el, rec); }
    rec[a] = { en, shown: out };
    if (out !== value) el.setAttribute(a, out);
  }
}

function walk(root) {
  if (root.nodeType === 3) {
    if (!skipped(root.parentElement)) doText(root);
    return;
  }
  if (root.nodeType !== 1 || skipped(root)) return;
  doAttrs(root);
  const tw = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
    acceptNode: (n) => (n.nodeType === 1 && skipped(n) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
  });
  for (let n = tw.nextNode(); n; n = tw.nextNode()) {
    if (n.nodeType === 3) doText(n);
    else doAttrs(n);
  }
}

function observe() {
  if (observer) return;
  observer = new MutationObserver((list) => {
    if (current === "en" && !exact) return;
    for (const m of list) {
      if (m.type === "characterData") {
        if (!skipped(m.target.parentElement)) doText(m.target);
      } else if (m.type === "attributes") {
        if (!skipped(m.target)) doAttrs(m.target);
      } else {
        m.addedNodes.forEach(walk);
      }
    }
  });
  observer.observe(document.body, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ATTRS });
}

// setLanguage switches the interface language ("" = follow the system). Safe to call again.
export async function setLanguage(code) {
  const want = code || systemLanguage();
  let dict = null;
  if (want !== "en") {
    const load = loaders[`./locales/${want}.json`];
    try { dict = load ? (await load()).default : null; } catch { dict = null; }
  }
  current = dict ? want : "en";
  if (dict) compile(dict); else { exact = null; patterns = []; }
  document.documentElement.lang = current;
  observe();
  walk(document.body); // retranslate what's on screen (or restore the English)
  return current;
}
