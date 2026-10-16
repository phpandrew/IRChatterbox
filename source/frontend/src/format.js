// mIRC formatting → HTML, link detection, nick colours, inline media.

export function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

const MIRC = [
  "#ffffff", "#000000", "#00007f", "#009300", "#ff0000", "#7f0000", "#9c009c", "#fc7f00",
  "#ffff00", "#00fc00", "#009393", "#00ffff", "#0000fc", "#ff00ff", "#7f7f7f", "#d2d2d2",
];
// 16-98 extended palette
const EXT = "470000 472100 474700 324700 004700 00472c 004747 002747 000047 2e0047 470047 47002a 740000 743a00 747400 517400 007400 007449 007474 004074 000074 4b0074 740074 740045 b50000 b56300 b5b500 7db500 00b500 00b571 00b5b5 0063b5 0000b5 7500b5 b500b5 b5006b ff0000 ff8c00 ffff00 b2ff00 00ff00 00ffa0 00ffff 008cff 0000ff a500ff ff00ff ff0098 ff5959 ffb459 ffff71 cfff60 6fff6f 65ffc9 6dffff 59b4ff 5959ff c459ff ff66ff ff59bc ff9c9c ffd39c ffff9c e2ff9c 9cff9c 9cffdb 9cffff 9cd3ff 9c9cff dc9cff ff9cff ff94d3 000000 131313 282828 363636 4d4d4d 656565 818181 9f9f9f bcbcbc e2e2e2 ffffff"
  .split(" ").map((h) => "#" + h);
function mircColor(n) {
  n = parseInt(n, 10);
  if (isNaN(n) || n === 99) return null;
  if (n < 16) return MIRC[n];
  return EXT[n - 16] || null;
}

const URL_RE = /\b((?:https?:\/\/|www\.)[^\s<>"'\x00-\x1f]+[^\s<>"'.,;:!?)\]}\x00-\x1f])/gi;

export function findUrls(text) {
  const plain = stripCodes(text);
  const out = [];
  for (const m of plain.matchAll(URL_RE)) out.push(m[1].startsWith("www.") ? "http://" + m[1] : m[1]);
  return out;
}

export function stripCodes(s) {
  return String(s).replace(/\x03(\d{1,2}(,\d{1,2})?)?|\x04[0-9a-fA-F]{6}(,[0-9a-fA-F]{6})?|[\x02\x0f\x16\x1d\x1f\x1e\x11]/g, "");
}

function escapeRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Render a text segment: escape, linkify, decorate mentions.
function renderPlain(text, ctx) {
  let out = "";
  let last = 0;
  for (const m of text.matchAll(URL_RE)) {
    out += decorate(text.slice(last, m.index), ctx);
    const href = m[1].startsWith("www.") ? "http://" + m[1] : m[1];
    out += `<a href="${esc(href)}" data-url="${esc(href)}">${esc(m[1])}</a>`;
    last = m.index + m[1].length;
  }
  out += decorate(text.slice(last), ctx);
  return out;
}

function decorate(text, ctx) {
  if (!text) return "";
  if (!ctx.mentionRe) return esc(text);
  let out = "";
  let last = 0;
  for (const m of text.matchAll(ctx.mentionRe)) {
    const word = m[2];
    const start = m.index + m[1].length;
    out += esc(text.slice(last, start));
    out += `<span class="mention">${esc(word)}</span>`;
    last = start + word.length;
  }
  return out + esc(text.slice(last));
}

export function mentionRegex(words) {
  const list = words.filter(Boolean).map(escapeRe);
  if (!list.length) return null;
  return new RegExp(`(^|[^\\p{L}\\p{N}_])(@?(?:${list.join("|")}))(?=$|[^\\p{L}\\p{N}_])`, "giu");
}

// Convert IRC formatted text to HTML.
export function ircToHtml(text, ctx = {}) {
  let html = "";
  let st = { b: 0, i: 0, u: 0, s: 0, m: 0, fg: null, bg: null, rev: 0 };
  let buf = "";
  const flush = () => {
    if (!buf) return;
    const cls = [];
    if (st.b) cls.push("f-b");
    if (st.i) cls.push("f-i");
    if (st.u) cls.push("f-u");
    if (st.s) cls.push("f-s");
    if (st.m) cls.push("f-m");
    let fg = st.fg, bg = st.bg;
    if (st.rev) [fg, bg] = [bg || "var(--bg)", fg || "var(--text)"];
    const style = (fg ? `color:${fg};` : "") + (bg ? `background:${bg};` : "");
    const inner = renderPlain(buf, ctx);
    html += cls.length || style ? `<span${cls.length ? ` class="${cls.join(" ")}"` : ""}${style ? ` style="${style}"` : ""}>${inner}</span>` : inner;
    buf = "";
  };
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    switch (ch) {
      case "\x02": flush(); st.b ^= 1; break;
      case "\x1d": flush(); st.i ^= 1; break;
      case "\x1f": flush(); st.u ^= 1; break;
      case "\x1e": flush(); st.s ^= 1; break;
      case "\x11": flush(); st.m ^= 1; break;
      case "\x16": flush(); st.rev ^= 1; break;
      case "\x0f": flush(); st = { b: 0, i: 0, u: 0, s: 0, m: 0, fg: null, bg: null, rev: 0 }; break;
      case "\x03": {
        flush();
        const m = /^(\d{1,2})(?:,(\d{1,2}))?/.exec(text.slice(i + 1));
        if (!m) { st.fg = st.bg = null; break; }
        st.fg = mircColor(m[1]);
        if (m[2] !== undefined) st.bg = mircColor(m[2]);
        i += m[0].length;
        break;
      }
      case "\x04": {
        flush();
        const m = /^([0-9a-fA-F]{6})(?:,([0-9a-fA-F]{6}))?/.exec(text.slice(i + 1));
        if (!m) { st.fg = st.bg = null; break; }
        st.fg = "#" + m[1];
        if (m[2]) st.bg = "#" + m[2];
        i += m[0].length;
        break;
      }
      default:
        buf += ch;
    }
  }
  flush();
  return html;
}

// ---------- nick colours ----------
let overrides = {};
export function setNickOverrides(map) { overrides = map || {}; }

function hash(s) {
  let h = 5381;
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) >>> 0;
  return h;
}
// 18 distinct hues, skipping muddy ranges
const HUES = [0, 18, 32, 45, 88, 130, 155, 172, 188, 200, 212, 228, 258, 275, 292, 310, 325, 342];
// The "person" behind a nick, ignoring away/alt suffixes, so these all share a colour:
// Andrew, Andrew|Slop, Andrew|away, Andrew_, Andrew__, Andrew^, Andrew`, Andrew-, Andrew[afk]
export function baseNick(nick) {
  let n = String(nick || "").toLowerCase();
  n = n.split("|")[0];
  n = n.replace(/\[[^\]]*\]$/, "");
  n = n.replace(/[_`^\-]+$/, "");
  return n || String(nick || "").toLowerCase();
}

export function nickColor(nick) {
  if (!nick) return "var(--text)";
  const o = overrides[nick.toLowerCase()] || overrides[baseNick(nick)];
  if (o) return o;
  const h = HUES[hash(baseNick(nick)) % HUES.length];
  return `hsl(${h} var(--nick-s) var(--nick-l))`;
}

// ---------- inline media ----------
const IMG_RE = /\.(png|jpe?g|gif|webp|avif|bmp)$/i;
const YT_RE = /(?:youtube\.com\/(?:watch\?(?:[^#]*&)?v=|shorts\/|live\/|embed\/)|youtu\.be\/|youtube-nocookie\.com\/embed\/)([\w-]{11})/i;

export function mediaFor(url) {
  let u;
  try { u = new URL(url); } catch { return null; }
  const yt = YT_RE.exec(url);
  if (yt) {
    const t = u.searchParams.get("t") || u.searchParams.get("start");
    let start = 0;
    if (t) {
      const mm = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s?)?$/.exec(t);
      if (mm) start = (+mm[1] || 0) * 3600 + (+mm[2] || 0) * 60 + (+mm[3] || 0);
    }
    return { type: "youtube", id: yt[1], start };
  }
  if (IMG_RE.test(u.pathname)) return { type: "image", src: url, name: decodeURIComponent(u.pathname.split("/").pop() || "") };
  // imgur single image pages: imgur.com/abc123 → i.imgur.com/abc123.jpg
  if (/^(www\.)?imgur\.com$/i.test(u.hostname) && /^\/[A-Za-z0-9]{5,8}$/.test(u.pathname))
    return { type: "image", src: `https://i.imgur.com${u.pathname}.jpg`, name: u.pathname.slice(1) };
  return null;
}
