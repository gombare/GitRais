import { describe, expect, it } from 'vitest';
import { commandFor, matches, defaultKeymap } from './keymap';

const k = (key: string, mods: Partial<{ metaKey: boolean; shiftKey: boolean; altKey: boolean; ctrlKey: boolean }> = {}) =>
  ({ key, metaKey: false, shiftKey: false, altKey: false, ctrlKey: false, ...mods });

describe('keymap', () => {
  it('matches exact modifiers', () => {
    expect(matches(k('Enter', { metaKey: true }), 'Cmd+Enter')).toBe(true);
    expect(matches(k('Enter', { metaKey: true, shiftKey: true }), 'Cmd+Enter')).toBe(false);
    expect(matches(k('b', { metaKey: true }), 'Cmd+B')).toBe(true);
    expect(matches(k('B', { metaKey: true, shiftKey: true }), 'Cmd+Shift+B')).toBe(true);
    expect(matches(k('b', { ctrlKey: true }), 'Cmd+B')).toBe(false);
  });
  it('finds the command', () => {
    expect(commandFor(k('Enter', { metaKey: true, shiftKey: true }))).toBe('commit:commitAll');
    expect(commandFor(k('ArrowDown'))).toBe('select:next');
    expect(commandFor(k('x'))).toBeNull();
  });
  it('has no duplicate accelerators', () => {
    const v = Object.values(defaultKeymap);
    expect(new Set(v).size).toBe(v.length);
  });
});
