// Built-in themes. Custom themes are JSON files in %APPDATA%\IRChatterbox\themes
// and use the same variable names.

export const THEME_VARS = [
  ["bg", "Chat background"], ["bg-sidebar", "Sidebars"], ["bg-elev", "Popups & menus"],
  ["bg-input", "Inputs"], ["bg-hover", "Hover"], ["bg-active", "Selected buffer"],
  ["border", "Borders"], ["text", "Text"], ["text-muted", "Muted text"], ["text-faint", "Faint text"],
  ["accent", "Accent"], ["accent-text", "Text on accent"], ["hl-bg", "Highlight line bg"],
  ["hl-border", "Highlight line edge"], ["mention-bg", "Mention chip"], ["link", "Links"],
  ["danger", "Errors / ping badge"], ["success", "Connected"], ["warn", "Connecting"],
];

export const BUILTIN = {
  Dark: {
    name: "Dark", base: "dark", builtin: true,
    vars: {
      "bg": "#15181e", "bg-sidebar": "#1a1e25", "bg-elev": "#20252d", "bg-input": "#1d2129",
      "bg-hover": "#242a33", "bg-active": "#1e3a4a", "border": "#2a3039", "text": "#e3e6eb",
      "text-muted": "#9aa3ae", "text-faint": "#6b7480", "accent": "#4cc2ff", "accent-text": "#06202e",
      "hl-bg": "#2a2519", "hl-border": "#ffb84c", "mention-bg": "#173445", "link": "#6cc8ff",
      "danger": "#ff6b6b", "success": "#3ccf7e", "warn": "#f2b53c",
    },
  },
  Light: {
    name: "Light", base: "light", builtin: true,
    vars: {
      "bg": "#ffffff", "bg-sidebar": "#f3f4f6", "bg-elev": "#ffffff", "bg-input": "#f7f8fa",
      "bg-hover": "#e9ebef", "bg-active": "#dbeafe", "border": "#e1e4e9", "text": "#1c2028",
      "text-muted": "#535b67", "text-faint": "#8a929e", "accent": "#0f6cbd", "accent-text": "#ffffff",
      "hl-bg": "#fff4dc", "hl-border": "#e08a00", "mention-bg": "#dceafb", "link": "#0b62b0",
      "danger": "#d13438", "success": "#138a43", "warn": "#c77c00",
    },
  },
  Midnight: {
    name: "Midnight", base: "dark", builtin: true,
    vars: {
      "bg": "#0d1117", "bg-sidebar": "#10151d", "bg-elev": "#161b22", "bg-input": "#0f141b",
      "bg-hover": "#1a212b", "bg-active": "#1f2b3d", "border": "#21262d", "text": "#d7dde5",
      "text-muted": "#8b949e", "text-faint": "#5c6570", "accent": "#a78bfa", "accent-text": "#1b1130",
      "hl-bg": "#2a1f12", "hl-border": "#f0883e", "mention-bg": "#2a2245", "link": "#9ab8ff",
      "danger": "#f85149", "success": "#3fb950", "warn": "#d29922",
    },
  },
  Classic: {
    name: "Classic", base: "light", builtin: true,
    vars: {
      "bg": "#eeeeee", "bg-sidebar": "#e2e2e2", "bg-elev": "#f7f7f7", "bg-input": "#ffffff",
      "bg-hover": "#d8d8d8", "bg-active": "#c8d8ee", "border": "#c4c4c4", "text": "#111111",
      "text-muted": "#444444", "text-faint": "#777777", "accent": "#2a5db0", "accent-text": "#ffffff",
      "hl-bg": "#fdf0c8", "hl-border": "#d08000", "mention-bg": "#d6e3f7", "link": "#1a3fbf",
      "danger": "#c0262c", "success": "#1f8f3a", "warn": "#b07000",
    },
  },
};

let custom = {};
export function setCustomThemes(list) {
  custom = {};
  for (const t of list || []) custom[t.name] = t;
}
export function allThemes() {
  return { ...BUILTIN, ...custom };
}
export function getTheme(name) {
  return custom[name] || BUILTIN[name];
}

const darkMQ = window.matchMedia("(prefers-color-scheme: dark)");
export function systemIsDark() {
  return darkMQ.matches;
}
export function onSystemThemeChange(fn) {
  darkMQ.addEventListener("change", fn);
}

// Resolve the theme to use from settings (auto follows Windows app mode).
export function resolveTheme(settings) {
  let name = settings.theme || "auto";
  if (name === "auto") name = systemIsDark() ? settings.darkTheme || "Dark" : settings.lightTheme || "Light";
  if (name === "dark") name = "Dark";
  if (name === "light") name = "Light";
  return getTheme(name) || (systemIsDark() ? BUILTIN.Dark : BUILTIN.Light);
}

export function applyTheme(theme) {
  const root = document.documentElement;
  const base = theme.base === "light" ? BUILTIN.Light.vars : BUILTIN.Dark.vars;
  for (const [k] of THEME_VARS) root.style.setProperty("--" + k, theme.vars?.[k] || base[k]);
  const light = theme.base === "light";
  root.style.setProperty("--nick-l", light ? "38%" : "68%");
  root.style.setProperty("--nick-s", light ? "70%" : "62%");
  root.style.colorScheme = light ? "light" : "dark";
  root.dataset.base = light ? "light" : "dark";
}
