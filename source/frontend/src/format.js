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

// ---------- keeping custom colours readable ----------
// A custom colour like black looks great in light mode but vanishes in dark mode. When
// enabled, colours with poor contrast against the chat background get their lightness
// flipped (black → white, navy → light blue), keeping the hue. Readable colours are untouched.
let chatBg = null; // [r,g,b] of the current chat background
let fixContrast = true;
const fixedCache = new Map();

export function setNickContrast(bgHex, enabled) {
  chatBg = parseHex(bgHex);
  fixContrast = enabled !== false;
  fixedCache.clear();
}

function parseHex(c) {
  const m = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(String(c || "").trim());
  if (!m) return null;
  let h = m[1];
  if (h.length === 3) h = h.split("").map((x) => x + x).join("");
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
}
function luminance([r, g, b]) {
  const f = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}
export function contrastRatio(a, b) {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}
function rgbToHsl([r, g, b]) {
  r /= 255; g /= 255; b /= 255;
  const max = Math.max(r, g, b), min = Math.min(r, g, b), l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min, s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  const h = max === r ? (g - b) / d + (g < b ? 6 : 0) : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return [h * 60, s, l];
}
function hslToRgb([h, s, l]) {
  const k = (n) => (n + h / 30) % 12, a = s * Math.min(l, 1 - l);
  const f = (n) => l - a * Math.max(-1, Math.min(k(n) - 3, 9 - k(n), 1));
  return [f(0), f(8), f(4)].map((v) => Math.round(v * 255));
}
const toHex = (rgb) => "#" + rgb.map((v) => v.toString(16).padStart(2, "0")).join("");

const MIN_CONTRAST = 3; // WCAG minimum for large/bold text — nicks are bold

export function readableColor(color) {
  if (!fixContrast || !chatBg) return color;
  if (fixedCache.has(color)) return fixedCache.get(color);
  const rgb = parseHex(color);
  let out = color;
  if (rgb && contrastRatio(rgb, chatBg) < MIN_CONTRAST) {
    const [h, s] = rgbToHsl(rgb);
    let l = 1 - rgbToHsl(rgb)[2]; // flip lightness: black ↔ white, navy ↔ light blue
    const darkBg = luminance(chatBg) < 0.5;
    // still too close? keep moving away from the background until it's readable
    for (let i = 0; i < 20 && contrastRatio(hslToRgb([h, s, l]), chatBg) < MIN_CONTRAST; i++) {
      l = darkBg ? Math.min(1, l + 0.05) : Math.max(0, l - 0.05);
    }
    out = toHex(hslToRgb([h, s, l]));
  }
  fixedCache.set(color, out);
  return out;
}

export function nickColor(nick) {
  if (!nick) return "var(--text)";
  const o = overrides[nick.toLowerCase()] || overrides[baseNick(nick)];
  if (o) return readableColor(o);
  const h = HUES[hash(baseNick(nick)) % HUES.length];
  return `hsl(${h} var(--nick-s) var(--nick-l))`;
}

// ---------- inline media ----------
const IMG_RE = /\.(png|jpe?g|gif|webp|avif|bmp|svg|ico|jfif)$/i;
const VIDEO_RE = /\.(mp4|webm|mov|m4v|ogv)$/i;
const AUDIO_RE = /\.(mp3|ogg|oga|opus|wav|m4a|aac|flac)$/i;
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
  const name = (() => { try { return decodeURIComponent(u.pathname.split("/").pop() || ""); } catch { return u.pathname.split("/").pop() || ""; } })();
  const host = u.hostname.replace(/^www\./i, "").toLowerCase();
  // imgur .gifv is really a silent looping mp4
  if (/(^|\.)imgur\.com$/.test(host) && /\.gifv$/i.test(u.pathname))
    return { type: "video", src: `https://i.imgur.com${u.pathname.replace(/\.gifv$/i, ".mp4")}`, name, gif: true };
  // Giphy pages: giphy.com/gifs/some-title-ID → the gif itself
  const gp = host === "giphy.com" && /^\/(?:gifs|stickers|embed)\/(?:[\w-]*-)?([A-Za-z0-9]{8,})\/?$/.exec(u.pathname);
  if (gp) return { type: "image", src: `https://media.giphy.com/media/${gp[1]}/giphy.gif`, name: "Giphy" };
  if (IMG_RE.test(u.pathname)) return { type: "image", src: url, name };
  if (VIDEO_RE.test(u.pathname)) return { type: "video", src: url, name };
  if (AUDIO_RE.test(u.pathname)) return { type: "audio", src: url, name };
  // click-to-play embeds
  // Spotify: open.spotify.com/(intl-xx/)track|album|playlist|episode|show|artist/ID
  const sp = host === "open.spotify.com" && /^\/(?:intl-[\w-]+\/)?(track|album|playlist|episode|show|artist)\/([A-Za-z0-9]{22})\/?$/.exec(u.pathname);
  if (sp) {
    const kind = sp[1][0].toUpperCase() + sp[1].slice(1);
    return { type: "embed", id: `spotify:${sp[1]}:${sp[2]}`, provider: "Spotify", kind, link: url,
      src: `https://open.spotify.com/embed/${sp[1]}/${sp[2]}?utm_source=generator`,
      height: sp[1] === "track" || sp[1] === "episode" ? 152 : 352 };
  }
  const vm = host === "vimeo.com" && /^\/(\d{5,})\/?$/.exec(u.pathname);
  if (vm) return { type: "embed", id: "vimeo:" + vm[1], provider: "Vimeo", src: `https://player.vimeo.com/video/${vm[1]}?autoplay=1`, link: url };
  const st = host === "streamable.com" && /^\/([a-z0-9]{4,})\/?$/i.exec(u.pathname);
  if (st) return { type: "embed", id: "streamable:" + st[1], provider: "Streamable", src: `https://streamable.com/e/${st[1]}?autoplay=1`, link: url };
  // imgur single image pages: imgur.com/abc123 → i.imgur.com/abc123.jpg
  if (/^(www\.)?imgur\.com$/i.test(u.hostname) && /^\/[A-Za-z0-9]{5,8}$/.test(u.pathname))
    return { type: "image", src: `https://i.imgur.com${u.pathname}.jpg`, name: u.pathname.slice(1) };
  return null;
}

// The letters on a server's badge: what was typed (up to 2, as typed), or the name's first letter.
export function iconLetters(text, name) {
  const typed = Array.from((text || "").replace(/\s+/g, "")).slice(0, 2).join("");
  return typed || (Array.from((name || "").trim() || "?")[0] || "?").toUpperCase();
}
