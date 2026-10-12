import { useEffect, useRef, useState } from 'react';
import { create } from 'zustand';

type Base = { opener: HTMLElement | null };
type Dialog =
  | (Base & { kind: 'text'; title: string; label?: string; initial?: string; resolve: (v: string | null) => void })
  | (Base & { kind: 'confirm'; title: string; message: string; danger?: boolean; resolve: (v: boolean) => void });

const useDialog = create<{ d: Dialog | null }>(() => ({ d: null }));

// Only one dialog at a time: opening a new one cancels the one still showing.
function show(d: Dialog) {
  const prev = useDialog.getState().d;
  if (prev) (prev.resolve as (v: null | false) => void)(prev.kind === 'text' ? null : false);
  useDialog.setState({ d });
}

const opener = () => (document.activeElement instanceof HTMLElement ? document.activeElement : null);

export function askText(o: { title: string; label?: string; initial?: string }): Promise<string | null> {
  return new Promise((resolve) => show({ kind: 'text', ...o, resolve, opener: opener() }));
}

export function confirmAction(o: { title: string; message: string; danger?: boolean }): Promise<boolean> {
  return new Promise((resolve) => show({ kind: 'confirm', ...o, resolve, opener: opener() }));
}

const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function DialogHost() {
  const d = useDialog((s) => s.d);
  const [value, setValue] = useState('');
  const input = useRef<HTMLInputElement>(null);
  const root = useRef<HTMLFormElement>(null);
  useEffect(() => {
    if (d?.kind === 'text') {
      setValue(d.initial ?? '');
      const t = setTimeout(() => input.current?.select(), 0);
      return () => clearTimeout(t);
    }
  }, [d]);
  if (!d) return null;
  const close = (ok: boolean) => {
    useDialog.setState({ d: null });
    if (d.kind === 'text') d.resolve(ok && value.trim() ? value.trim() : null);
    else d.resolve(ok);
    d.opener?.focus(); // return focus to whatever opened the dialog
  };
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      close(false);
    } else if (e.key === 'Tab') {
      // Focus trap: wrap around inside the dialog.
      const els = Array.from(root.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? []);
      if (!els.length) return;
      const first = els[0];
      const last = els[els.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  };
  const dangerConfirm = d.kind === 'confirm' && !!d.danger;
  const empty = d.kind === 'text' && !value.trim();
  return (
    <div className="modal-backdrop" onMouseDown={() => close(false)}>
      <form ref={root} className="modal" role="dialog" aria-modal="true" aria-label={d.title}
        onMouseDown={(e) => e.stopPropagation()}
        onSubmit={(e) => { e.preventDefault(); if (!empty) close(true); }}
        onKeyDown={onKeyDown}>
        <h3>{d.title}</h3>
        {d.kind === 'text'
          ? <label>{d.label}<input ref={input} autoFocus value={value} onChange={(e) => setValue(e.target.value)} /></label>
          : <p>{d.message}</p>}
        <div className="modal-actions">
          {/* Destructive confirms start on Cancel so a stray Enter cannot confirm. */}
          <button type="button" autoFocus={dangerConfirm} onClick={() => close(false)}>Annulla</button>
          <button type="submit" autoFocus={d.kind === 'confirm' && !dangerConfirm} disabled={empty}
            className={d.kind === 'confirm' && d.danger ? 'danger' : 'primary'}>OK</button>
        </div>
      </form>
    </div>
  );
}
