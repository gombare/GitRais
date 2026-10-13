// The Monaco theme matching the app's palette. Kept free of Monaco imports so it can be
// unit-tested and imported by DiffView without pulling Monaco into the main chunk.

export const MONACO_THEME = 'gitrais';

// Defaults mirror styles.css :root, used if a token is missing or not a plain hex colour.
const DEFAULTS: Record<string, string> = {
  '--bg': '#16181d', '--bg-2': '#1d2026', '--bg-3': '#252932', '--border': '#2f3440',
  '--fg': '#d8dce4', '--fg-muted': '#9aa2b1', '--accent': '#4fa3ff', '--danger': '#ff6b6b', '--ok': '#4cd08a',
};

// Normalises "#abc" / "#aabbcc" to "#aabbcc"; anything else yields the default.
function hex(v: string, fallback: string): string {
  const s = v.trim().toLowerCase();
  if (/^#[0-9a-f]{6}$/.test(s)) return s;
  if (/^#[0-9a-f]{3}$/.test(s)) return `#${s[1]}${s[1]}${s[2]}${s[2]}${s[3]}${s[3]}`;
  return fallback;
}

// "#rrggbb" plus an alpha in [0, 1] as Monaco's "#rrggbbaa".
const alpha = (c: string, a: number) => c + Math.round(a * 255).toString(16).padStart(2, '0');

export type ThemeData = { base: 'vs-dark'; inherit: boolean; rules: []; colors: Record<string, string> };

// `token` reads a CSS custom property (e.g. from getComputedStyle(document.documentElement)).
export function gitraisTheme(token: (name: string) => string): ThemeData {
  const t = (name: string) => hex(token(name) ?? '', DEFAULTS[name]);
  const bg = t('--bg'), bg2 = t('--bg-2'), bg3 = t('--bg-3'), border = t('--border');
  const fg = t('--fg'), muted = t('--fg-muted'), accent = t('--accent'), ok = t('--ok'), danger = t('--danger');
  return {
    base: 'vs-dark',
    inherit: true,
    rules: [],
    colors: {
      'editor.background': bg,
      'editor.foreground': fg,
      'editorGutter.background': bg,
      'editorLineNumber.foreground': alpha(muted, 0.7),
      'editorLineNumber.activeForeground': fg,
      'editor.lineHighlightBackground': alpha(bg2, 0.6),
      'editor.lineHighlightBorder': alpha(bg2, 0),
      'editor.selectionBackground': alpha(accent, 0.3),
      'editor.inactiveSelectionBackground': alpha(accent, 0.15),
      'editorWidget.background': bg2,
      'editorWidget.border': border,
      'editorHoverWidget.background': bg3,
      'editorHoverWidget.border': border,
      'focusBorder': alpha(accent, 0.6),
      'scrollbar.shadow': alpha(bg, 0),
      'scrollbarSlider.background': alpha(muted, 0.2),
      'scrollbarSlider.hoverBackground': alpha(muted, 0.3),
      'scrollbarSlider.activeBackground': alpha(muted, 0.4),
      // Diff colours: the app's ok/danger hues, much less saturated than vs-dark.
      'diffEditor.insertedTextBackground': alpha(ok, 0.2),
      'diffEditor.removedTextBackground': alpha(danger, 0.2),
      'diffEditor.insertedLineBackground': alpha(ok, 0.08),
      'diffEditor.removedLineBackground': alpha(danger, 0.08),
      'diffEditorGutter.insertedLineBackground': alpha(ok, 0.14),
      'diffEditorGutter.removedLineBackground': alpha(danger, 0.14),
      'diffEditor.diagonalFill': alpha(border, 0.6),
      'diffEditor.border': border,
      'editorOverviewRuler.border': alpha(bg, 0),
    },
  };
}
