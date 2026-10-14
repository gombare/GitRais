import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { create } from 'zustand';

// title: a tooltip, e.g. why an item is disabled.
export type MenuItem = { label: string; onClick: () => void; danger?: boolean; disabled?: boolean; title?: string } | 'separator';

type MenuState = { x: number; y: number; items: MenuItem[] | null; opener: HTMLElement | null };
const useMenu = create<MenuState>(() => ({ x: 0, y: 0, items: null, opener: null }));

export function openMenu(e: React.MouseEvent, items: MenuItem[]) {
  e.preventDefault();
  e.stopPropagation();
  let { clientX: x, clientY: y } = e;
  // Keyboard-invoked context menus report 0,0: anchor them to the element instead.
  if (x === 0 && y === 0 && e.currentTarget instanceof HTMLElement) {
    const r = e.currentTarget.getBoundingClientRect();
    x = r.left + 8;
    y = r.bottom;
  }
  const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  useMenu.setState({ x, y, items, opener });
}

// Keyboard entry point (ContextMenu key, Shift+F10): the menu is anchored below `anchor`.
export function openMenuAt(anchor: HTMLElement, items: MenuItem[]) {
  const r = anchor.getBoundingClientRect();
  const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  useMenu.setState({ x: r.left + 8, y: r.bottom, items, opener });
}

// Drops leading, trailing and repeated separators, so menus can build optional groups freely.
export function tidy(items: MenuItem[]): MenuItem[] {
  const out: MenuItem[] = [];
  for (const it of items) {
    if (it === 'separator' && (out.length === 0 || out.at(-1) === 'separator')) continue;
    out.push(it);
  }
  if (out.at(-1) === 'separator') out.pop();
  return out;
}

function closeMenu(restoreFocus: boolean) {
  const { opener } = useMenu.getState();
  useMenu.setState({ items: null, opener: null });
  if (restoreFocus) opener?.focus();
}

export function ContextMenuHost() {
  const { x, y, items } = useMenu();
  const ref = useRef<HTMLUListElement>(null);
  const [pos, setPos] = useState({ left: x, top: y });

  useEffect(() => {
    const close = () => closeMenu(false);
    window.addEventListener('mousedown', close);
    window.addEventListener('blur', close);
    return () => { window.removeEventListener('mousedown', close); window.removeEventListener('blur', close); };
  }, []);

  // Keep the menu inside the viewport, then focus the first enabled item.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !items) return;
    const { width, height } = el.getBoundingClientRect();
    setPos({
      left: Math.max(0, Math.min(x, window.innerWidth - width - 4)),
      top: Math.max(0, Math.min(y, window.innerHeight - height - 4)),
    });
    el.querySelector<HTMLElement>('[role="menuitem"]:not([aria-disabled="true"])')?.focus();
  }, [items, x, y]);

  if (!items) return null;
  const shown = tidy(items);

  const enabled = () => Array.from(ref.current?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([aria-disabled="true"])') ?? []);
  const move = (delta: number) => {
    const els = enabled();
    if (!els.length) return;
    const i = els.indexOf(document.activeElement as HTMLElement);
    els[(i + delta + els.length) % els.length].focus();
  };
  const activate = (it: Exclude<MenuItem, 'separator'>) => {
    if (it.disabled) return;
    closeMenu(true);
    it.onClick();
  };
  const onKeyDown = (e: React.KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown': e.preventDefault(); move(1); break;
      case 'ArrowUp': e.preventDefault(); move(-1); break;
      case 'Home': e.preventDefault(); enabled()[0]?.focus(); break;
      case 'End': e.preventDefault(); enabled().at(-1)?.focus(); break;
      case 'Escape': e.preventDefault(); closeMenu(true); break;
      case 'Tab': e.preventDefault(); closeMenu(true); break;
      case 'Enter':
      case ' ': {
        e.preventDefault();
        (document.activeElement as HTMLElement | null)?.click();
        break;
      }
    }
  };
  return (
    <ul ref={ref} role="menu" className="ctx-menu" style={{ left: pos.left, top: pos.top }}
      onMouseDown={(e) => e.stopPropagation()} onKeyDown={onKeyDown}>
      {shown.map((it, i) => it === 'separator'
        ? <li key={i} role="separator" className="ctx-sep" />
        : (
          <li key={i} role="menuitem" tabIndex={-1} aria-disabled={it.disabled || undefined} title={it.title}
            className={`${it.danger ? 'danger' : ''} ${it.disabled ? 'disabled' : ''}`}
            onClick={() => activate(it)}>
            {it.label}
          </li>
        ))}
    </ul>
  );
}
