// Emoji picker: button beside Send, popover with search, categories and recently used.
// Data (unicode-emoji-json, MIT) is bundled and loaded on first open, so it works offline.

const ICONS = {
  recent: "🕘", smileys_emotion: "😀", people_body: "👋", animals_nature: "🐻", food_drink: "🍔",
  travel_places: "🚗", activities: "⚽", objects: "💡", symbols: "❤️", flags: "🏁",
};
const MAX_VERSION = 15.0; // newer emoji don't render on all Windows versions yet

let groups = null;
let box = null;
let input = null;
let onInsert = null;

function loadRecent() {
  try { return JSON.parse(localStorage.getItem("emojiRecent") || "[]"); } catch { return []; }
}
function saveRecent(e) {
  const r = [e, ...loadRecent().filter((x) => x !== e)].slice(0, 32);
  try { localStorage.setItem("emojiRecent", JSON.stringify(r)); } catch {}
}

async function loadData() {
  if (groups) return groups;
  const data = (await import("unicode-emoji-json/data-by-group.json")).default;
  groups = data
    .filter((g) => g.slug !== "component")
    .map((g) => ({ slug: g.slug, name: g.name, emojis: g.emojis.filter((e) => parseFloat(e.emoji_version) <= MAX_VERSION) }));
  return groups;
}

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function cell(e, name) {
  return `<button class="em" data-e="${esc(e)}" title="${esc(name || "")}">${e}</button>`;
}

function render(query) {
  const body = box.querySelector(".em-body");
  const q = query.trim().toLowerCase();
  if (q) {
    const hits = [];
    for (const g of groups) for (const e of g.emojis) if (e.name.includes(q) || e.slug.includes(q.replace(/\s+/g, "_"))) hits.push(e);
    body.innerHTML = hits.length
      ? `<div class="em-grid">${hits.slice(0, 300).map((e) => cell(e.emoji, e.name)).join("")}</div>`
      : `<div class="em-none">No emoji match “${esc(query)}”</div>`;
    return;
  }
  const recent = loadRecent();
  let html = "";
  if (recent.length) html += `<div class="em-head" id="em-recent">Recently used</div><div class="em-grid">${recent.map((e) => cell(e)).join("")}</div>`;
  for (const g of groups) html += `<div class="em-head" id="em-${g.slug}">${esc(g.name)}</div><div class="em-grid">${g.emojis.map((e) => cell(e.emoji, e.name)).join("")}</div>`;
  body.innerHTML = html;
}

export async function toggleEmojiPicker(anchor, textarea, insert) {
  if (box && !box.classList.contains("hidden")) return closeEmojiPicker();
  input = textarea;
  onInsert = insert;
  if (!box) {
    box = document.createElement("div");
    box.id = "emojiPicker";
    box.innerHTML = `<div class="em-top"><input class="em-search" placeholder="Search emoji…" spellcheck="false"></div>
      <div class="em-tabs"></div><div class="em-body"><div class="em-none">Loading…</div></div>
      <div class="em-foot"><span class="em-preview"></span><span class="em-hint">Shift+click to add several</span></div>`;
    document.body.appendChild(box);
    const search = box.querySelector(".em-search");
    search.addEventListener("input", () => render(search.value));
    search.addEventListener("keydown", (e) => {
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); closeEmojiPicker(); }
      if (e.key === "Enter") { const first = box.querySelector(".em"); if (first) { e.preventDefault(); pick(first.dataset.e, false); } }
    });
    box.addEventListener("mousedown", (e) => { if (e.target.closest(".em,.em-tab")) e.preventDefault(); });
    box.addEventListener("click", (e) => {
      const b = e.target.closest(".em");
      if (b) return pick(b.dataset.e, e.shiftKey);
      const t = e.target.closest(".em-tab");
      if (t) {
        search.value = "";
        render("");
        box.querySelector("#em-" + t.dataset.g)?.scrollIntoView({ block: "start" });
      }
    });
    box.addEventListener("mouseover", (e) => {
      const b = e.target.closest(".em");
      if (b) box.querySelector(".em-preview").innerHTML = `<span class="big">${b.dataset.e}</span>${esc(b.title)}`;
    });
    document.addEventListener("mousedown", (e) => {
      if (box && !box.classList.contains("hidden") && !box.contains(e.target) && !e.target.closest("#emojiBtn")) closeEmojiPicker();
    });
  }
  box.classList.remove("hidden");
  position(anchor);
  await loadData();
  const tabs = [...(loadRecent().length ? [["recent", "Recently used"]] : []), ...groups.map((g) => [g.slug, g.name])];
  box.querySelector(".em-tabs").innerHTML = tabs.map(([s, n]) => `<button class="em-tab" data-g="${s}" title="${esc(n)}">${ICONS[s] || "•"}</button>`).join("");
  const search = box.querySelector(".em-search");
  search.value = "";
  render("");
  box.querySelector(".em-body").scrollTop = 0;
  search.focus();
}

function position(anchor) {
  const r = anchor.getBoundingClientRect();
  const w = 352;
  box.style.left = Math.max(8, Math.min(r.right - w, innerWidth - w - 8)) + "px";
  // above the button, or below it when there isn't room (reacting to a message near the top)
  const h = box.offsetHeight || 420;
  if (r.top - 8 < h && innerHeight - r.bottom > r.top) {
    box.style.bottom = "auto";
    box.style.top = Math.min(r.bottom + 8, innerHeight - h - 8) + "px";
  } else {
    box.style.top = "auto";
    box.style.bottom = innerHeight - r.top + 8 + "px";
  }
}

function pick(e, keepOpen) {
  saveRecent(e);
  onInsert?.(e);
  if (!keepOpen) closeEmojiPicker();
}

export function closeEmojiPicker() {
  if (!box) return;
  box.classList.add("hidden");
  input?.focus();
}
