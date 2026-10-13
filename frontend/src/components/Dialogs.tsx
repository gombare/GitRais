import { useEffect, useRef, useState } from 'react';
import { create } from 'zustand';

// confirmLabel names the action on the confirm button ("Crea branch", "Elimina tag"...); default "OK".
type Base = { opener: HTMLElement | null; confirmLabel?: string };
type TextOptions = {
  title: string;
  confirmLabel?: string;
  label?: string;
  initial?: string;
  // OK is enabled with an empty value too (it resolves ''); Cancel still resolves null.
  allowEmpty?: boolean;
};
type Check = { label: string; initial: boolean };
export type TextCheck = { text: string; checked: boolean };
type Dialog =
  | (Base & TextOptions & { kind: 'text'; check?: Check; resolve: (v: TextCheck | null) => void })
  | (Base & { kind: 'choice'; title: string; message: string; options: { value: string; label: string }[]; resolve: (v: string | null) => void })
  | (Base & ConfirmOptions & { kind: 'confirm'; resolve: (v: boolean) => void });
type ConfirmOptions = {
  title: string;
  message: string;
  danger?: boolean;
  confirmLabel?: string;
  // Shown as a scrollable monospace list under the message (e.g. the files a discard touches).
  items?: string[];
};

const useDialog = create<{ d: Dialog | null }>(() => ({ d: null }));

// Only one dialog at a time: opening a new one cancels the one still showing.
function show(d: Dialog) {
  const prev = useDialog.getState().d;
  if (prev) (prev.resolve as (v: null | false) => void)(prev.kind === 'confirm' ? false : null);
  useDialog.setState({ d });
}

const opener = () => (document.activeElement instanceof HTMLElement ? document.activeElement : null);

export function askText(o: TextOptions): Promise<string | null> {
  return new Promise((resolve) => show({ kind: 'text', ...o, resolve: (v) => resolve(v ? v.text : null), opener: opener() }));
}

// Text field plus one checkbox (e.g. "checkout the new branch"); resolves both, or null if cancelled.
export function askTextCheck(o: TextOptions & { check: Check }): Promise<TextCheck | null> {
  return new Promise((resolve) => show({ kind: 'text', ...o, resolve, opener: opener() }));
}

// Lets the user pick one of several options; resolves with its value, or null if cancelled.
export function chooseOption(o: { title: string; message: string; options: { value: string; label: string }[] }): Promise<string | null> {
  return new Promise((resolve) => show({ kind: 'choice', ...o, resolve, opener: opener() }));
}

export function confirmAction(o: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => show({ kind: 'confirm', ...o, resolve, opener: opener() }));
}

const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function DialogHost() {
  const d = useDialog((s) => s.d);
  const [value, setValue] = useState('');
  const [checked, setChecked] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const root = useRef<HTMLFormElement>(null);
  useEffect(() => {
    if (d?.kind === 'text') {
      setValue(d.initial ?? '');
      setChecked(d.check?.initial ?? false);
      const t = setTimeout(() => input.current?.select(), 0);
      return () => clearTimeout(t);
    }
  }, [d]);
  if (!d) return null;
  const close = (ok: boolean) => {
    useDialog.setState({ d: null });
    if (d.kind === 'text') d.resolve(ok && (value.trim() || d.allowEmpty) ? { text: value.trim(), checked } : null);
    else if (d.kind === 'choice') d.resolve(null);
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
  const pick = (v: string) => {
    useDialog.setState({ d: null });
    (d as Extract<Dialog, { kind: 'choice' }>).resolve(v);
    d.opener?.focus();
  };
  const dangerConfirm = d.kind === 'confirm' && !!d.danger;
  const empty = d.kind === 'text' && !d.allowEmpty && !value.trim();
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
        {d.kind === 'confirm' && d.items && d.items.length > 0 && (
          <ul className="modal-list" aria-label="Elementi interessati" tabIndex={0}>
            {d.items.map((it, i) => <li key={i}>{it}</li>)}
          </ul>
        )}
        {d.kind === 'text' && d.check && (
          <label className="modal-check">
            <input type="checkbox" checked={checked} onChange={(e) => setChecked(e.target.checked)} />
            {d.check.label}
          </label>
        )}
        {d.kind === 'choice' && (
          <div className="modal-choices">
            {d.options.map((o, i) => <button key={o.value} type="button" autoFocus={i === 0} onClick={() => pick(o.value)}>{o.label}</button>)}
          </div>
        )}
        <div className="modal-actions">
          {/* Destructive confirms start on Cancel so a stray Enter cannot confirm. */}
          <button type="button" autoFocus={dangerConfirm} onClick={() => close(false)}>Annulla</button>
          {d.kind !== 'choice' && <button type="submit" autoFocus={d.kind === 'confirm' && !dangerConfirm} disabled={empty}
            className={d.kind === 'confirm' && d.danger ? 'danger' : 'primary'}>{d.confirmLabel || 'OK'}</button>}
        </div>
      </form>
    </div>
  );
}
