// Release notes come from GitHub as markdown. This draws the common subset safely: every
// character is escaped first, and only https links become links (opened in the browser).
// Headings, lists (one level of nesting), bold, italics, inline code, code blocks, rules and
// paragraphs; anything else shows as plain text.
import { esc } from "./format.js";

const safeUrl = (u) => /^https:\/\/[^\s"'<>]+$/i.test(u) ? u : "";

function inline(raw) {
  // pull out code spans first so nothing inside them is formatted
  const codes = [];
  let s = raw.replace(/`([^`]+)`/g, (_, c) => { codes.push(c); return `\u0000${codes.length - 1}\u0000`; });
  const links = [];
  const keep = (html) => { links.push(html); return `\u0001${links.length - 1}\u0001`; };
  // [text](https://…)
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (m, label, url) => {
    const u = safeUrl(url);
    return u ? keep(`<a href="${esc(u)}" data-url="${esc(u)}">${esc(label)}</a>`) : m;
  });
  // bare https URLs; GitHub's pull request and compare links get short labels
  s = s.replace(/https:\/\/[^\s<>()]+[^\s<>().,;:!?'"]/g, (url) => {
    const u = safeUrl(url);
    if (!u) return url;
    const pr = u.match(/^https:\/\/github\.com\/[^/]+\/[^/]+\/(?:pull|issues)\/(\d+)$/);
    const cmp = u.match(/^https:\/\/github\.com\/[^/]+\/[^/]+\/compare\/(.+)$/);
    const label = pr ? `#${pr[1]}` : cmp ? cmp[1] : u;
    return keep(`<a href="${esc(u)}" data-url="${esc(u)}">${esc(label)}</a>`);
  });
  s = esc(s);
  s = s.replace(/\*\*(.+?)\*\*/g, "<b>$1</b>").replace(/__(.+?)__/g, "<b>$1</b>");
  s = s.replace(/(^|[\s(])\*(?!\s)(.+?)\*(?=$|[\s).,;:!?])/g, "$1<i>$2</i>")
    .replace(/(^|[\s(])_(?!\s)(.+?)_(?=$|[\s).,;:!?])/g, "$1<i>$2</i>");
  s = s.replace(/~~(.+?)~~/g, "<s>$1</s>");
  s = s.replace(/\u0001(\d+)\u0001/g, (_, i) => links[+i]);
  s = s.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${esc(codes[+i])}</code>`);
  return s;
}

export function notesToHtml(md) {
  const lines = String(md || "").replace(/\r\n?/g, "\n").replace(/<!--[\s\S]*?-->/g, "").split("\n");
  const out = [];
  let para = [];
  let lists = []; // open list tags, innermost last: { tag, indent }
  let fence = null;
  const flushPara = () => { if (para.length) { out.push(`<p>${para.map(inline).join("<br>")}</p>`); para = []; } };
  const closeLists = (toDepth = 0) => { while (lists.length > toDepth) out.push(`</li></${lists.pop().tag}>`); };
  for (const line of lines) {
    if (fence !== null) {
      if (/^\s*```/.test(line)) { out.push(`<pre><code>${esc(fence.join("\n"))}</code></pre>`); fence = null; }
      else fence.push(line);
      continue;
    }
    if (/^\s*```/.test(line)) { flushPara(); closeLists(); fence = []; continue; }
    if (!line.trim()) { flushPara(); continue; }
    const h = line.match(/^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/);
    if (h) { flushPara(); closeLists(); const n = Math.min(h[1].length + 2, 6); out.push(`<h${n}>${inline(h[2])}</h${n}>`); continue; }
    if (/^\s{0,3}([-*_])(\s*\1){2,}\s*$/.test(line)) { flushPara(); closeLists(); out.push("<hr>"); continue; }
    const li = line.match(/^(\s*)([-*+]|\d+[.)])\s+(.*)$/);
    if (li) {
      flushPara();
      const indent = li[1].replace(/\t/g, "  ").length;
      const tag = /\d/.test(li[2]) ? "ol" : "ul";
      const item = inline(li[3].replace(/^\[( |x|X)\]\s+/, (m, c) => (c === " " ? "☐ " : "☑ ")));
      const top = lists[lists.length - 1];
      if (!top) { lists.push({ tag, indent }); out.push(`<${tag}><li>${item}`); }
      else if (indent > top.indent && lists.length < 3) { lists.push({ tag, indent }); out.push(`<${tag}><li>${item}`); }
      else {
        while (lists.length > 1 && indent < lists[lists.length - 1].indent) out.push(`</li></${lists.pop().tag}>`);
        const cur = lists[lists.length - 1];
        if (cur.tag !== tag) { out.push(`</li></${cur.tag}><${tag}><li>${item}`); cur.tag = tag; cur.indent = indent; }
        else out.push(`</li><li>${item}`);
      }
      continue;
    }
    const quote = line.match(/^\s*>\s?(.*)$/);
    if (quote) { flushPara(); closeLists(); out.push(`<blockquote>${inline(quote[1])}</blockquote>`); continue; }
    if (lists.length && /^\s+\S/.test(line)) { out.push(" " + inline(line.trim())); continue; } // continues the list item
    closeLists();
    para.push(line.trim());
  }
  if (fence !== null) out.push(`<pre><code>${esc(fence.join("\n"))}</code></pre>`);
  flushPara();
  closeLists();
  return out.join("");
}
