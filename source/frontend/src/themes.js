// Built-in themes. Custom themes are JSON files in %APPDATA%\IRChatterbox\themes
// and use the same variable names.

export const THEME_VARS = [
  ["bg", "Chat background"], ["bg-sidebar", "Sidebars"], ["bg-elev", "Popups & menus"],
  ["bg-input", "Inputs"], ["bg-hover", "Hover"], ["bg-active", "Selected buffer"],
  ["border", "Borders"], ["text", "Text"], ["text-muted", "Muted text"], ["text-faint", "Faint text"],
  ["accent", "Accent"], ["accent-text", "Text on accent"], ["hl-bg", "Highlight line bg"],
  ["hl-border", "Highlight line edge"], ["mention-bg", "Mention chip"], ["link", "Links"],
  ["danger", "Errors / ping badge"], ["success", "Connected"], ["warn", "Connecting"],
  ["unread", "Channel with new messages"],
];

export const BUILTIN = {
  Dark: {
    name: "Dark", group: "IRChatterbox", base: "dark", builtin: true,
    vars: {
      "bg": "#15181e", "bg-sidebar": "#1a1e25", "bg-elev": "#20252d", "bg-input": "#1d2129",
      "bg-hover": "#242a33", "bg-active": "#1e3a4a", "border": "#2a3039", "text": "#e3e6eb",
      "text-muted": "#9aa3ae", "text-faint": "#6b7480", "accent": "#4cc2ff", "accent-text": "#06202e",
      "hl-bg": "#2a2519", "hl-border": "#ffb84c", "mention-bg": "#173445", "link": "#6cc8ff",
      "danger": "#ff6b6b", "success": "#3ccf7e", "warn": "#f2b53c",
    },
  },
  Light: {
    name: "Light", group: "IRChatterbox", base: "light", builtin: true,
    vars: {
      "bg": "#ffffff", "bg-sidebar": "#f3f4f6", "bg-elev": "#ffffff", "bg-input": "#f7f8fa",
      "bg-hover": "#e9ebef", "bg-active": "#dbeafe", "border": "#e1e4e9", "text": "#1c2028",
      "text-muted": "#535b67", "text-faint": "#8a929e", "accent": "#0f6cbd", "accent-text": "#ffffff",
      "hl-bg": "#fff4dc", "hl-border": "#e08a00", "mention-bg": "#dceafb", "link": "#0b62b0",
      "danger": "#d13438", "success": "#138a43", "warn": "#c77c00",
    },
  },
  Midnight: {
    name: "Midnight", group: "IRChatterbox", base: "dark", builtin: true,
    vars: {
      "bg": "#0d1117", "bg-sidebar": "#10151d", "bg-elev": "#161b22", "bg-input": "#0f141b",
      "bg-hover": "#1a212b", "bg-active": "#1f2b3d", "border": "#21262d", "text": "#d7dde5",
      "text-muted": "#8b949e", "text-faint": "#5c6570", "accent": "#a78bfa", "accent-text": "#1b1130",
      "hl-bg": "#2a1f12", "hl-border": "#f0883e", "mention-bg": "#2a2245", "link": "#9ab8ff",
      "danger": "#f85149", "success": "#3fb950", "warn": "#d29922",
    },
  },
  Classic: {
    name: "Classic", group: "IRChatterbox", base: "light", builtin: true,
    vars: {
      "bg": "#eeeeee", "bg-sidebar": "#e2e2e2", "bg-elev": "#f7f7f7", "bg-input": "#ffffff",
      "bg-hover": "#d8d8d8", "bg-active": "#c8d8ee", "border": "#c4c4c4", "text": "#111111",
      "text-muted": "#444444", "text-faint": "#777777", "accent": "#2a5db0", "accent-text": "#ffffff",
      "hl-bg": "#fdf0c8", "hl-border": "#d08000", "mention-bg": "#d6e3f7", "link": "#1a3fbf",
      "danger": "#c0262c", "success": "#1f8f3a", "warn": "#b07000",
    },
  },
};


// Popular editor/terminal palettes and throwback looks. "style" adds extra CSS (see style.css,
// :root[data-style=…]) for looks colours alone can't give, like Windows 98 bevels.
Object.assign(BUILTIN, {
  "Dracula": {
    "name": "Dracula",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#282a36",
      "bg-sidebar": "#21222c",
      "bg-elev": "#343746",
      "bg-input": "#21222c",
      "bg-hover": "#3a3c4e",
      "bg-active": "#44475a",
      "border": "#3a3c4e",
      "text": "#f8f8f2",
      "text-muted": "#b4b6c4",
      "text-faint": "#6272a4",
      "accent": "#bd93f9",
      "accent-text": "#282a36",
      "hl-bg": "#3b3a2c",
      "hl-border": "#ffb86c",
      "mention-bg": "#44475a",
      "link": "#8be9fd",
      "danger": "#ff5555",
      "success": "#50fa7b",
      "warn": "#f1fa8c"
    }
  },
  "Nord": {
    "name": "Nord",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#2e3440",
      "bg-sidebar": "#2a2f3a",
      "bg-elev": "#3b4252",
      "bg-input": "#3b4252",
      "bg-hover": "#3b4252",
      "bg-active": "#434c5e",
      "border": "#434c5e",
      "text": "#eceff4",
      "text-muted": "#d8dee9",
      "text-faint": "#7b88a1",
      "accent": "#88c0d0",
      "accent-text": "#2e3440",
      "hl-bg": "#3d3b35",
      "hl-border": "#ebcb8b",
      "mention-bg": "#394a5c",
      "link": "#88c0d0",
      "danger": "#bf616a",
      "success": "#a3be8c",
      "warn": "#ebcb8b"
    }
  },
  "One Dark": {
    "name": "One Dark",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#282c34",
      "bg-sidebar": "#21252b",
      "bg-elev": "#2c313a",
      "bg-input": "#1e2227",
      "bg-hover": "#2c313a",
      "bg-active": "#3a3f4b",
      "border": "#3a3f4b",
      "text": "#d7dae0",
      "text-muted": "#abb2bf",
      "text-faint": "#5c6370",
      "accent": "#61afef",
      "accent-text": "#1e2227",
      "hl-bg": "#3a3426",
      "hl-border": "#e5c07b",
      "mention-bg": "#2f3d52",
      "link": "#61afef",
      "danger": "#e06c75",
      "success": "#98c379",
      "warn": "#e5c07b"
    }
  },
  "Monokai": {
    "name": "Monokai",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#272822",
      "bg-sidebar": "#1e1f1c",
      "bg-elev": "#3e3d32",
      "bg-input": "#1e1f1c",
      "bg-hover": "#3e3d32",
      "bg-active": "#49483e",
      "border": "#3e3d32",
      "text": "#f8f8f2",
      "text-muted": "#cfcfc2",
      "text-faint": "#75715e",
      "accent": "#a6e22e",
      "accent-text": "#272822",
      "hl-bg": "#3e3a24",
      "hl-border": "#e6db74",
      "mention-bg": "#49483e",
      "link": "#66d9ef",
      "danger": "#f92672",
      "success": "#a6e22e",
      "warn": "#fd971f"
    }
  },
  "Gruvbox Dark": {
    "name": "Gruvbox Dark",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#282828",
      "bg-sidebar": "#1d2021",
      "bg-elev": "#3c3836",
      "bg-input": "#32302f",
      "bg-hover": "#3c3836",
      "bg-active": "#504945",
      "border": "#3c3836",
      "text": "#ebdbb2",
      "text-muted": "#bdae93",
      "text-faint": "#928374",
      "accent": "#fabd2f",
      "accent-text": "#282828",
      "hl-bg": "#3c3420",
      "hl-border": "#fe8019",
      "mention-bg": "#45403d",
      "link": "#83a598",
      "danger": "#fb4934",
      "success": "#b8bb26",
      "warn": "#fabd2f"
    }
  },
  "Catppuccin Mocha": {
    "name": "Catppuccin Mocha",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#1e1e2e",
      "bg-sidebar": "#181825",
      "bg-elev": "#313244",
      "bg-input": "#181825",
      "bg-hover": "#313244",
      "bg-active": "#45475a",
      "border": "#313244",
      "text": "#cdd6f4",
      "text-muted": "#a6adc8",
      "text-faint": "#6c7086",
      "accent": "#cba6f7",
      "accent-text": "#1e1e2e",
      "hl-bg": "#3a3328",
      "hl-border": "#fab387",
      "mention-bg": "#3b3552",
      "link": "#89b4fa",
      "danger": "#f38ba8",
      "success": "#a6e3a1",
      "warn": "#f9e2af"
    }
  },
  "Tokyo Night": {
    "name": "Tokyo Night",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#1a1b26",
      "bg-sidebar": "#16161e",
      "bg-elev": "#24283b",
      "bg-input": "#16161e",
      "bg-hover": "#292e42",
      "bg-active": "#2f3549",
      "border": "#292e42",
      "text": "#c0caf5",
      "text-muted": "#a9b1d6",
      "text-faint": "#565f89",
      "accent": "#7aa2f7",
      "accent-text": "#16161e",
      "hl-bg": "#33302a",
      "hl-border": "#e0af68",
      "mention-bg": "#2b3456",
      "link": "#7dcfff",
      "danger": "#f7768e",
      "success": "#9ece6a",
      "warn": "#e0af68"
    }
  },
  "Solarized Dark": {
    "name": "Solarized Dark",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#002b36",
      "bg-sidebar": "#00232c",
      "bg-elev": "#073642",
      "bg-input": "#073642",
      "bg-hover": "#0a3d4a",
      "bg-active": "#0e4a58",
      "border": "#0a4250",
      "text": "#93a1a1",
      "text-muted": "#839496",
      "text-faint": "#586e75",
      "accent": "#268bd2",
      "accent-text": "#fdf6e3",
      "hl-bg": "#2b3a1f",
      "hl-border": "#b58900",
      "mention-bg": "#0d4058",
      "link": "#2aa198",
      "danger": "#dc322f",
      "success": "#859900",
      "warn": "#b58900"
    }
  },
  "GitHub Dimmed": {
    "name": "GitHub Dimmed",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#22272e",
      "bg-sidebar": "#1c2128",
      "bg-elev": "#2d333b",
      "bg-input": "#1c2128",
      "bg-hover": "#2d333b",
      "bg-active": "#373e47",
      "border": "#444c56",
      "text": "#adbac7",
      "text-muted": "#909dab",
      "text-faint": "#768390",
      "accent": "#539bf5",
      "accent-text": "#1c2128",
      "hl-bg": "#3a3324",
      "hl-border": "#c69026",
      "mention-bg": "#253650",
      "link": "#539bf5",
      "danger": "#e5534b",
      "success": "#57ab5a",
      "warn": "#c69026"
    }
  },
  "Rosé Pine": {
    "name": "Rosé Pine",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#191724",
      "bg-sidebar": "#1f1d2e",
      "bg-elev": "#26233a",
      "bg-input": "#1f1d2e",
      "bg-hover": "#21202e",
      "bg-active": "#403d52",
      "border": "#26233a",
      "text": "#e0def4",
      "text-muted": "#908caa",
      "text-faint": "#6e6a86",
      "accent": "#c4a7e7",
      "accent-text": "#191724",
      "hl-bg": "#3a3026",
      "hl-border": "#f6c177",
      "mention-bg": "#2e2a45",
      "link": "#9ccfd8",
      "danger": "#eb6f92",
      "success": "#9ccfd8",
      "warn": "#f6c177"
    }
  },
  "Everforest": {
    "name": "Everforest",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#2d353b",
      "bg-sidebar": "#232a2e",
      "bg-elev": "#343f44",
      "bg-input": "#232a2e",
      "bg-hover": "#3d484d",
      "bg-active": "#475258",
      "border": "#3d484d",
      "text": "#d3c6aa",
      "text-muted": "#a7a08c",
      "text-faint": "#859289",
      "accent": "#a7c080",
      "accent-text": "#2d353b",
      "hl-bg": "#3e3c2e",
      "hl-border": "#dbbc7f",
      "mention-bg": "#3b4a40",
      "link": "#7fbbb3",
      "danger": "#e67e80",
      "success": "#a7c080",
      "warn": "#dbbc7f"
    }
  },
  "High Contrast": {
    "name": "High Contrast",
    "base": "dark",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#000000",
      "bg-sidebar": "#000000",
      "bg-elev": "#0d0d0d",
      "bg-input": "#000000",
      "bg-hover": "#1f1f1f",
      "bg-active": "#1aebff",
      "border": "#ffffff",
      "text": "#ffffff",
      "text-muted": "#ffffff",
      "text-faint": "#c8c8c8",
      "accent": "#1aebff",
      "accent-text": "#000000",
      "hl-bg": "#332b00",
      "hl-border": "#ffff00",
      "mention-bg": "#003b40",
      "link": "#ffff00",
      "danger": "#ff4d4d",
      "success": "#3ff23f",
      "warn": "#ffff00"
    }
  },
  "Solarized Light": {
    "name": "Solarized Light",
    "base": "light",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#fdf6e3",
      "bg-sidebar": "#eee8d5",
      "bg-elev": "#fdf6e3",
      "bg-input": "#fffbef",
      "bg-hover": "#e8e1cc",
      "bg-active": "#dde6e0",
      "border": "#e3dcc6",
      "text": "#586e75",
      "text-muted": "#657b83",
      "text-faint": "#93a1a1",
      "accent": "#268bd2",
      "accent-text": "#fdf6e3",
      "hl-bg": "#f6ebc8",
      "hl-border": "#b58900",
      "mention-bg": "#dbe9f0",
      "link": "#268bd2",
      "danger": "#dc322f",
      "success": "#859900",
      "warn": "#b58900"
    }
  },
  "Gruvbox Light": {
    "name": "Gruvbox Light",
    "base": "light",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#fbf1c7",
      "bg-sidebar": "#f2e5bc",
      "bg-elev": "#f9f5d7",
      "bg-input": "#f9f5d7",
      "bg-hover": "#ebdbb2",
      "bg-active": "#d5c4a1",
      "border": "#d5c4a1",
      "text": "#3c3836",
      "text-muted": "#665c54",
      "text-faint": "#928374",
      "accent": "#076678",
      "accent-text": "#fbf1c7",
      "hl-bg": "#f6e3a8",
      "hl-border": "#af3a03",
      "mention-bg": "#dde4d4",
      "link": "#076678",
      "danger": "#9d0006",
      "success": "#79740e",
      "warn": "#b57614"
    }
  },
  "Catppuccin Latte": {
    "name": "Catppuccin Latte",
    "base": "light",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#eff1f5",
      "bg-sidebar": "#e6e9ef",
      "bg-elev": "#eff1f5",
      "bg-input": "#e6e9ef",
      "bg-hover": "#dce0e8",
      "bg-active": "#ccd0da",
      "border": "#ccd0da",
      "text": "#4c4f69",
      "text-muted": "#6c6f85",
      "text-faint": "#9ca0b0",
      "accent": "#8839ef",
      "accent-text": "#eff1f5",
      "hl-bg": "#fbebd6",
      "hl-border": "#fe640b",
      "mention-bg": "#e4dcf7",
      "link": "#1e66f5",
      "danger": "#d20f39",
      "success": "#40a02b",
      "warn": "#df8e1d"
    }
  },
  "GitHub Light": {
    "name": "GitHub Light",
    "base": "light",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#ffffff",
      "bg-sidebar": "#f6f8fa",
      "bg-elev": "#ffffff",
      "bg-input": "#f6f8fa",
      "bg-hover": "#eaeef2",
      "bg-active": "#ddf4ff",
      "border": "#d0d7de",
      "text": "#1f2328",
      "text-muted": "#59636e",
      "text-faint": "#818b98",
      "accent": "#0969da",
      "accent-text": "#ffffff",
      "hl-bg": "#fff8c5",
      "hl-border": "#9a6700",
      "mention-bg": "#ddf4ff",
      "link": "#0969da",
      "danger": "#cf222e",
      "success": "#1a7f37",
      "warn": "#9a6700"
    }
  },
  "Rosé Pine Dawn": {
    "name": "Rosé Pine Dawn",
    "base": "light",
    "builtin": true,
    "group": "Popular",
    "vars": {
      "bg": "#faf4ed",
      "bg-sidebar": "#fffaf3",
      "bg-elev": "#fffaf3",
      "bg-input": "#fffaf3",
      "bg-hover": "#f2e9e1",
      "bg-active": "#dfdad9",
      "border": "#dfdad9",
      "text": "#575279",
      "text-muted": "#797593",
      "text-faint": "#9893a5",
      "accent": "#907aa9",
      "accent-text": "#faf4ed",
      "hl-bg": "#f8ead2",
      "hl-border": "#ea9d34",
      "mention-bg": "#ece4f0",
      "link": "#286983",
      "danger": "#b4637a",
      "success": "#56949f",
      "warn": "#ea9d34"
    }
  },
  "Windows 98": {
    "name": "Windows 98",
    "base": "light",
    "builtin": true,
    "group": "Throwback",
    "vars": {
      "bg": "#ffffff",
      "bg-sidebar": "#c0c0c0",
      "bg-elev": "#c0c0c0",
      "bg-input": "#ffffff",
      "bg-hover": "#d4d0c8",
      "bg-active": "#000080",
      "border": "#808080",
      "text": "#000000",
      "text-muted": "#222222",
      "text-faint": "#808080",
      "accent": "#000080",
      "accent-text": "#ffffff",
      "hl-bg": "#ffffcc",
      "hl-border": "#c00000",
      "mention-bg": "#c0c0ff",
      "link": "#0000ff",
      "danger": "#ff0000",
      "success": "#008000",
      "warn": "#808000"
    },
    "style": "win98",
    "font": "\"Tahoma\", \"MS Sans Serif\", \"Microsoft Sans Serif\", sans-serif"
  },
  "Windows XP": {
    "name": "Windows XP",
    "base": "light",
    "builtin": true,
    "group": "Throwback",
    "vars": {
      "bg": "#ffffff",
      "bg-sidebar": "#d6dff7",
      "bg-elev": "#ece9d8",
      "bg-input": "#ffffff",
      "bg-hover": "#c1d2ee",
      "bg-active": "#316ac5",
      "border": "#7f9db9",
      "text": "#000000",
      "text-muted": "#333333",
      "text-faint": "#808080",
      "accent": "#0054e3",
      "accent-text": "#ffffff",
      "hl-bg": "#fff4c2",
      "hl-border": "#e68b2c",
      "mention-bg": "#c1d2ee",
      "link": "#0033cc",
      "danger": "#c13d1a",
      "success": "#3c8d2e",
      "warn": "#e68b2c"
    },
    "style": "xp",
    "font": "\"Tahoma\", \"Segoe UI\", sans-serif"
  },
  "mIRC Classic": {
    "name": "mIRC Classic",
    "base": "light",
    "builtin": true,
    "group": "Throwback",
    "vars": {
      "bg": "#ffffff",
      "bg-sidebar": "#f0f0f0",
      "bg-elev": "#f0f0f0",
      "bg-input": "#ffffff",
      "bg-hover": "#e5e5e5",
      "bg-active": "#cce8ff",
      "border": "#a0a0a0",
      "text": "#000000",
      "text-muted": "#333333",
      "text-faint": "#7f7f7f",
      "accent": "#000080",
      "accent-text": "#ffffff",
      "hl-bg": "#fff0f0",
      "hl-border": "#ff0000",
      "mention-bg": "#ffe0e0",
      "link": "#0000ff",
      "danger": "#ff0000",
      "success": "#009300",
      "warn": "#fc7f00"
    },
    "style": "classic",
    "font": "\"Tahoma\", \"Segoe UI\", sans-serif",
    "chatFont": "\"Fixedsys Excelsior 3.01\", \"Fixedsys\", \"Consolas\", \"Courier New\", monospace"
  },
  "Terminal Green": {
    "name": "Terminal Green",
    "base": "dark",
    "builtin": true,
    "group": "Throwback",
    "vars": {
      "bg": "#050a05",
      "bg-sidebar": "#030703",
      "bg-elev": "#0b160b",
      "bg-input": "#071007",
      "bg-hover": "#0f200f",
      "bg-active": "#123a12",
      "border": "#174017",
      "text": "#33ff66",
      "text-muted": "#22c94a",
      "text-faint": "#1a7a2e",
      "accent": "#33ff66",
      "accent-text": "#001a00",
      "hl-bg": "#142a0a",
      "hl-border": "#ccff33",
      "mention-bg": "#0f3a12",
      "link": "#66ffcc",
      "danger": "#ff5555",
      "success": "#33ff66",
      "warn": "#ccff33"
    },
    "style": "crt",
    "crtHue": "60deg",
    "font": "\"Cascadia Mono\", \"Consolas\", monospace",
    "chatFont": "\"Cascadia Mono\", \"Consolas\", monospace"
  },
  "Amber CRT": {
    "name": "Amber CRT",
    "base": "dark",
    "builtin": true,
    "group": "Throwback",
    "vars": {
      "bg": "#0d0800",
      "bg-sidebar": "#080500",
      "bg-elev": "#1a1000",
      "bg-input": "#120b00",
      "bg-hover": "#241600",
      "bg-active": "#3d2600",
      "border": "#3d2600",
      "text": "#ffb000",
      "text-muted": "#d99400",
      "text-faint": "#8a5e00",
      "accent": "#ffb000",
      "accent-text": "#1a0f00",
      "hl-bg": "#2e1f00",
      "hl-border": "#ffd866",
      "mention-bg": "#3d2600",
      "link": "#ffcc55",
      "danger": "#ff6a3d",
      "success": "#ffb000",
      "warn": "#ffd866"
    },
    "style": "crt",
    "font": "\"Cascadia Mono\", \"Consolas\", monospace",
    "chatFont": "\"Cascadia Mono\", \"Consolas\", monospace"
  }
});

// Desktop-inspired themes: the look of macOS and of Ubuntu's Yaru (Linux).
Object.assign(BUILTIN, {
  "macOS Light": {
    name: "macOS Light", base: "light", builtin: true, group: "Desktop", style: "mac",
    font: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", \"Helvetica Neue\", \"Segoe UI\", sans-serif",
    vars: {
      "bg": "#ffffff", "bg-sidebar": "#ebebef", "bg-elev": "#ffffff", "bg-input": "#ffffff", "bg-hover": "#dfdfe5",
      "bg-active": "#d3d3da", "border": "#d1d1d6", "text": "#1d1d1f", "text-muted": "#55555c", "text-faint": "#8e8e93",
      "accent": "#007aff", "accent-text": "#ffffff", "hl-bg": "#fff5d9", "hl-border": "#ff9500", "mention-bg": "#d9e9ff",
      "link": "#0066cc", "danger": "#ff3b30", "success": "#28a745", "warn": "#ff9500",
    },
  },
  "macOS Dark": {
    name: "macOS Dark", base: "dark", builtin: true, group: "Desktop", style: "mac",
    font: "-apple-system, BlinkMacSystemFont, \"SF Pro Text\", \"Helvetica Neue\", \"Segoe UI\", sans-serif",
    vars: {
      "bg": "#1e1e1e", "bg-sidebar": "#2a2a2c", "bg-elev": "#2c2c2e", "bg-input": "#1c1c1e", "bg-hover": "#38383b",
      "bg-active": "#46464a", "border": "#3a3a3d", "text": "#f5f5f7", "text-muted": "#b0b0b5", "text-faint": "#78787e",
      "accent": "#0a84ff", "accent-text": "#ffffff", "hl-bg": "#3b3118", "hl-border": "#ff9f0a", "mention-bg": "#1d3a5e",
      "link": "#4aa3ff", "danger": "#ff453a", "success": "#30d158", "warn": "#ff9f0a",
    },
  },
  "Ubuntu Light": {
    name: "Ubuntu Light", base: "light", builtin: true, group: "Desktop", style: "yaru",
    font: "\"Ubuntu\", \"Ubuntu Sans\", \"Cantarell\", \"Segoe UI\", sans-serif",
    chatFont: "\"Ubuntu\", \"Ubuntu Sans\", \"Cantarell\", \"Segoe UI\", sans-serif",
    vars: {
      "bg": "#ffffff", "bg-sidebar": "#f6f6f6", "bg-elev": "#ffffff", "bg-input": "#ffffff", "bg-hover": "#e9e9e9",
      "bg-active": "#e95420", "border": "#d8d8d8", "text": "#3d3d3d", "text-muted": "#5e5e5e", "text-faint": "#8b8b8b",
      "accent": "#e95420", "accent-text": "#ffffff", "hl-bg": "#fdeee8", "hl-border": "#e95420", "mention-bg": "#f2dce9",
      "link": "#c34113", "danger": "#c7162b", "success": "#0e8420", "warn": "#f99b11",
    },
  },
  "Ubuntu Dark": {
    name: "Ubuntu Dark", base: "dark", builtin: true, group: "Desktop", style: "yaru",
    font: "\"Ubuntu\", \"Ubuntu Sans\", \"Cantarell\", \"Segoe UI\", sans-serif",
    chatFont: "\"Ubuntu\", \"Ubuntu Sans\", \"Cantarell\", \"Segoe UI\", sans-serif",
    vars: {
      "bg": "#2c2c2c", "bg-sidebar": "#242424", "bg-elev": "#363636", "bg-input": "#1e1e1e", "bg-hover": "#3a3a3a",
      "bg-active": "#e95420", "border": "#3f3f3f", "text": "#f7f7f7", "text-muted": "#c8c8c8", "text-faint": "#8d8d8d",
      "accent": "#e95420", "accent-text": "#ffffff", "hl-bg": "#40291f", "hl-border": "#f27f53", "mention-bg": "#5e2750",
      "link": "#ff8e5e", "danger": "#ef3f4f", "success": "#3eb34f", "warn": "#f99b11",
    },
  },
});

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

// A theme variable is a colour, nothing more: "url(…)" in a theme file would make the app
// fetch that address (and show the viewer's IP to whoever made the theme).
export function safeThemeValue(v) {
  v = String(v ?? "").trim();
  if (/^#[0-9a-f]{3,8}$/i.test(v) || /^[a-z]{3,20}$/i.test(v)) return v;
  if (/^(?:rgba?|hsla?)\([\d\s.,%/]+\)$/i.test(v)) return v;
  if (/^color-mix\(in srgb,\s*(?:#[0-9a-f]{3,8}|[a-z]{3,20})\s*\d{1,3}%?,\s*(?:#[0-9a-f]{3,8}|[a-z]{3,20})\s*\d{0,3}%?\)$/i.test(v)) return v;
  return "";
}

export function applyTheme(theme) {
  const root = document.documentElement;
  const base = theme.base === "light" ? BUILTIN.Light.vars : BUILTIN.Dark.vars;
  for (const [k] of THEME_VARS) {
    // "unread" is new: themes made before it keep their own text colour for unread channels
    const fallback = k === "unread" ? safeThemeValue(theme.vars?.text) || base.text : base[k];
    root.style.setProperty("--" + k, safeThemeValue(theme.vars?.[k]) || fallback);
  }
  const light = theme.base === "light";
  root.style.setProperty("--nick-l", light ? "38%" : "68%");
  root.style.setProperty("--nick-s", light ? "70%" : "62%");
  root.style.colorScheme = light ? "light" : "dark";
  root.dataset.base = light ? "light" : "dark";
  root.dataset.style = theme.style || "";
  const font = String(theme.font || "").replace(/[^\w\s,'"-]/g, "").trim(); // font names only
  if (font) root.style.setProperty("--font", font); else root.style.removeProperty("--font");
  root.style.setProperty("--theme-chat-font", theme.chatFont || "var(--font)");
  root.style.setProperty("--crt-hue", theme.crtHue || "0deg");
}

// <option>s grouped (IRChatterbox, Popular, Throwback, Custom); value = theme name.
export function themeOptions(selected, label = (n) => n) {
  const groups = {};
  for (const [n, t] of Object.entries(allThemes())) (groups[t.builtin ? t.group || "IRChatterbox" : "Custom"] ||= []).push(n);
  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
  return ["IRChatterbox", "Popular", "Desktop", "Throwback", "Custom"].filter((g) => groups[g]).map((g) =>
    `<optgroup label="${g}">${groups[g].map((n) => `<option value="${esc(n)}" ${n === selected ? "selected" : ""}>${esc(label(n))}</option>`).join("")}</optgroup>`).join("");
}
