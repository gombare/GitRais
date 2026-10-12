// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ContextMenuHost, openMenu, type MenuItem } from './ContextMenu';

afterEach(cleanup);

function setup(items: MenuItem[]) {
  render(<><button onContextMenu={(e) => openMenu(e, items)}>target</button><ContextMenuHost /></>);
  const target = screen.getByText('target');
  target.focus();
  fireEvent.contextMenu(target, { clientX: 10, clientY: 10 });
  return target;
}

describe('ContextMenu', () => {
  it('navigates with arrows, skips disabled items, Enter activates and returns focus', () => {
    const a = vi.fn(); const c = vi.fn(); const b = vi.fn();
    const target = setup([{ label: 'A', onClick: a }, { label: 'B', onClick: b, disabled: true }, 'separator', { label: 'C', onClick: c }]);
    const menu = screen.getByRole('menu');
    expect(document.activeElement).toBe(screen.getByText('A'));
    fireEvent.keyDown(menu, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(screen.getByText('C'));
    fireEvent.keyDown(menu, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(screen.getByText('A')); // wraps
    fireEvent.keyDown(menu, { key: 'ArrowUp' });
    expect(document.activeElement).toBe(screen.getByText('C'));
    fireEvent.keyDown(menu, { key: 'Enter' });
    expect(c).toHaveBeenCalledTimes(1);
    expect(a).not.toHaveBeenCalled();
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(target);
  });
  it('disabled items do not fire on click', () => {
    const b = vi.fn();
    setup([{ label: 'B', onClick: b, disabled: true }, { label: 'A', onClick: () => undefined }]);
    fireEvent.click(screen.getByText('B'));
    expect(b).not.toHaveBeenCalled();
  });
  it('Escape closes and restores focus', () => {
    const target = setup([{ label: 'A', onClick: () => undefined }]);
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(target);
  });
  it('closes on outside mousedown and on window blur', () => {
    setup([{ label: 'A', onClick: () => undefined }]);
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('menu')).toBeNull();
    fireEvent.contextMenu(screen.getByText('target'), { clientX: 1, clientY: 1 });
    expect(screen.getByRole('menu')).toBeTruthy();
    act(() => { window.dispatchEvent(new Event('blur')); });
    expect(screen.queryByRole('menu')).toBeNull();
  });
  it('stays within the viewport', () => {
    Object.defineProperty(window, 'innerWidth', { value: 500, configurable: true });
    Object.defineProperty(window, 'innerHeight', { value: 400, configurable: true });
    const spy = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(
      { width: 200, height: 100, top: 0, left: 0, right: 200, bottom: 100, x: 0, y: 0, toJSON() {} } as DOMRect);
    render(<><button onContextMenu={(e) => openMenu(e, [{ label: 'A', onClick: () => undefined }])}>t2</button><ContextMenuHost /></>);
    fireEvent.contextMenu(screen.getByText('t2'), { clientX: 490, clientY: 390 });
    const m = screen.getByRole('menu') as HTMLElement;
    expect(parseInt(m.style.left)).toBeLessThanOrEqual(500 - 200);
    expect(parseInt(m.style.top)).toBeLessThanOrEqual(400 - 100);
    spy.mockRestore();
  });
});
