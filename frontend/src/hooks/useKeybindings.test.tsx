// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/react';
import { useKeybindings } from './useKeybindings';
import type { Command } from '../keys/keymap';

function Host({ handlers }: { handlers: Partial<Record<Command, () => void>> }) {
  useKeybindings(handlers);
  return (
    <div>
      <input aria-label="field" />
      <textarea aria-label="area" />
      <div className="monaco-editor"><textarea aria-label="monaco" /></div>
      <button>btn</button>
    </div>
  );
}

afterEach(() => { cleanup(); document.body.innerHTML = ''; });

describe('useKeybindings', () => {
  it('runs the handler and prevents the default', () => {
    const h = vi.fn();
    render(<Host handlers={{ 'repo:fetch': h }} />);
    const ev = new KeyboardEvent('keydown', { key: 'f', metaKey: true, shiftKey: true, cancelable: true, bubbles: true });
    document.body.dispatchEvent(ev);
    expect(h).toHaveBeenCalledOnce();
    expect(ev.defaultPrevented).toBe(true);
  });
  it('ignores plain keys while typing in inputs, honours Cmd shortcuts', () => {
    const next = vi.fn();
    const commit = vi.fn();
    const { getByLabelText } = render(<Host handlers={{ 'select:next': next, 'commit:commit': commit }} />);
    fireEvent.keyDown(getByLabelText('field'), { key: 'ArrowDown' });
    fireEvent.keyDown(getByLabelText('area'), { key: 'ArrowDown' });
    expect(next).not.toHaveBeenCalled();
    fireEvent.keyDown(getByLabelText('area'), { key: 'Enter', metaKey: true });
    expect(commit).toHaveBeenCalledOnce();
  });
  it('ignores everything while the Monaco editor has focus', () => {
    const h = vi.fn();
    const { getByLabelText } = render(<Host handlers={{ 'repo:createBranch': h, 'select:next': h }} />);
    fireEvent.keyDown(getByLabelText('monaco'), { key: 'b', metaKey: true });
    fireEvent.keyDown(getByLabelText('monaco'), { key: 'ArrowDown' });
    expect(h).not.toHaveBeenCalled();
  });
  it('ignores shortcuts while a modal dialog is open', () => {
    const h = vi.fn();
    render(<Host handlers={{ 'repo:createBranch': h }} />);
    const m = document.createElement('div');
    m.className = 'modal-backdrop';
    document.body.appendChild(m);
    fireEvent.keyDown(document.body, { key: 'b', metaKey: true });
    expect(h).not.toHaveBeenCalled();
  });
  it('uses the latest handlers without re-subscribing', () => {
    const a = vi.fn();
    const b = vi.fn();
    const { rerender } = render(<Host handlers={{ 'view:toggleDetailPanel': a }} />);
    rerender(<Host handlers={{ 'view:toggleDetailPanel': b }} />);
    fireEvent.keyDown(document.body, { key: 'j', metaKey: true });
    expect(a).not.toHaveBeenCalled();
    expect(b).toHaveBeenCalledOnce();
  });
  it('does not preventDefault for commands without a handler', () => {
    render(<Host handlers={{}} />);
    const ev = new KeyboardEvent('keydown', { key: 'w', metaKey: true, cancelable: true, bubbles: true });
    document.body.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(false);
  });
  it('Cmd+Shift+S / Cmd+Shift+U do not fire inside a text field; other Cmd combos still do', () => {
    const s = vi.fn();
    const u = vi.fn();
    const fetch = vi.fn();
    const { getByLabelText } = render(<Host handlers={{ 'repo:stageAll': s, 'repo:unstageAll': u, 'repo:fetch': fetch }} />);
    fireEvent.keyDown(getByLabelText('field'), { key: 's', metaKey: true, shiftKey: true });
    fireEvent.keyDown(getByLabelText('area'), { key: 'u', metaKey: true, shiftKey: true });
    expect(s).not.toHaveBeenCalled();
    expect(u).not.toHaveBeenCalled();
    fireEvent.keyDown(getByLabelText('field'), { key: 'f', metaKey: true, shiftKey: true });
    expect(fetch).toHaveBeenCalledOnce();
    fireEvent.keyDown(document.body, { key: 's', metaKey: true, shiftKey: true });
    expect(s).toHaveBeenCalledOnce();
  });
});
