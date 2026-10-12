import { useEffect, useRef } from 'react';
import { commandFor, type Command } from '../keys/keymap';

// Registers window-level shortcuts. Menu items and shortcuts must dispatch the
// same handlers: callers pass one handler map and use it for both.
//  - Monaco has its own keymap: nothing is handled while it has focus.
//  - Inside a text field only Cmd shortcuts apply (so ArrowDown still moves the caret).
//  - Nothing is handled while a modal dialog is open.
export function useKeybindings(handlers: Partial<Record<Command, () => void>>) {
  const ref = useRef(handlers);
  ref.current = handlers;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.isComposing || e.defaultPrevented) return;
      const t = e.target instanceof Element ? e.target : null;
      if (t?.closest('.monaco-editor')) return;
      if (document.querySelector('.modal-backdrop')) return;
      const typing = t instanceof HTMLElement
        && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);
      if (typing && !e.metaKey) return;
      const cmd = commandFor(e);
      // Stage/unstage all must not hijack Cmd+Shift+S/U typed into a text field.
      if (typing && (cmd === 'repo:stageAll' || cmd === 'repo:unstageAll')) return;
      const h = cmd && ref.current[cmd];
      if (h) {
        e.preventDefault();
        h();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
}
