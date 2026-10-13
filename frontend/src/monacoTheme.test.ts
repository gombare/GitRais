import { describe, expect, it } from 'vitest';
import { gitraisTheme } from './monacoTheme';

const tokens: Record<string, string> = {
  '--bg': '#16181d', '--fg': ' #D8DCE4 ', '--ok': '#4cd08a', '--danger': '#f66', '--accent': 'rgb(1, 2, 3)',
};

describe('gitraisTheme', () => {
  const th = gitraisTheme((n) => tokens[n] ?? '');
  it('takes background and foreground from the CSS tokens', () => {
    expect(th.base).toBe('vs-dark');
    expect(th.colors['editor.background']).toBe('#16181d');
    expect(th.colors['editor.foreground']).toBe('#d8dce4');
  });
  it('derives translucent diff colours from ok/danger (3-digit hex expanded)', () => {
    expect(th.colors['diffEditor.insertedTextBackground']).toBe('#4cd08a33');
    expect(th.colors['diffEditor.removedTextBackground']).toBe('#ff666633');
    expect(th.colors['diffEditor.insertedLineBackground']).toBe('#4cd08a14');
  });
  it('falls back to the default palette for missing or non-hex tokens', () => {
    expect(th.colors['editor.selectionBackground']).toBe('#4fa3ff4d');
    expect(th.colors['editorWidget.background']).toBe('#1d2026');
  });
  it('only produces Monaco-compatible hex colours', () => {
    for (const v of Object.values(th.colors)) expect(v).toMatch(/^#[0-9a-f]{6}([0-9a-f]{2})?$/);
  });
});
