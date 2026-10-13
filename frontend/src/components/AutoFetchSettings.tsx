import { useEffect, useId, useRef, useState } from 'react';
import { api } from '../api/client';
import { useApp } from '../store/app';
import type { session, store } from '../../wailsjs/go/models';

const MIN_SECONDS = 30;

const reasons: Record<string, string> = {
  auth: 'autenticazione fallita',
  network: 'remote non raggiungibile',
};

// Toolbar control for the periodic background fetch: an on/off switch and the interval
// (global settings), plus whether it is paused for this tab after an auth/network failure.
export function AutoFetchSettings({ tab }: { tab: string }) {
  const toast = useApp((s) => s.toast);
  // A finished remote op (e.g. a manual fetch) may have resumed auto-fetch: re-read the status.
  const op = useApp((s) => !!s.ops[tab]);
  const [open, setOpen] = useState(false);
  const [settings, setSettings] = useState<store.Settings | null>(null);
  const [status, setStatus] = useState<session.AutoFetchStatus | null>(null);
  const [seconds, setSeconds] = useState('');
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const id = useId();

  useEffect(() => {
    let live = true;
    api.AutoFetchStatus(tab).then((s) => { if (live) setStatus(s); }, () => { /* tab closing: ignore */ });
    return () => { live = false; };
  }, [tab, op, open]);

  useEffect(() => {
    if (!open) return;
    let live = true;
    api.GetSettings().then((s) => {
      if (!live) return;
      setSettings(s);
      setSeconds(String(s.autoFetchSeconds));
    }, (e) => toast(e, tab));
    const onDown = (e: MouseEvent) => {
      if (!panel.current?.contains(e.target as Node) && !button.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    return () => { live = false; document.removeEventListener('mousedown', onDown); };
  }, [open, tab, toast]);

  const save = async (next: store.Settings) => {
    try {
      const saved = await api.SetSettings(next);
      setSettings(saved);
      setSeconds(String(saved.autoFetchSeconds));
      setStatus(await api.AutoFetchStatus(tab));
    } catch (e) {
      toast(e, tab);
    }
  };
  const commitSeconds = () => {
    if (!settings) return;
    const n = Math.max(MIN_SECONDS, Math.round(Number(seconds)) || settings.autoFetchSeconds);
    if (n === settings.autoFetchSeconds) {
      setSeconds(String(n));
      return;
    }
    return save({ ...settings, autoFetchSeconds: n } as store.Settings);
  };
  const close = () => {
    setOpen(false);
    button.current?.focus();
  };

  const paused = !!status?.enabled && !!status.paused;
  return (
    <span className="af">
      <button ref={button} aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? id : undefined}
        onClick={() => setOpen((o) => !o)} title="Fetch periodico in background dei remote">
        {paused ? 'Fetch automatico (in pausa)' : 'Fetch automatico'}
      </button>
      {open && (
        <div ref={panel} id={id} className="af-panel" role="dialog" aria-label="Fetch automatico"
          onKeyDown={(e) => { if (e.key === 'Escape') { e.preventDefault(); close(); } }}>
          {settings ? (
            <>
              <label className="modal-check">
                <input type="checkbox" autoFocus checked={settings.autoFetch}
                  onChange={(e) => save({ ...settings, autoFetch: e.target.checked } as store.Settings)} />
                Fetch automatico
              </label>
              <label>
                Intervallo (secondi, minimo {MIN_SECONDS})
                <input type="number" min={MIN_SECONDS} step={1} value={seconds} disabled={!settings.autoFetch}
                  onChange={(e) => setSeconds(e.target.value)} onBlur={commitSeconds}
                  onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); commitSeconds(); } }} />
              </label>
              <p className="muted">Non chiede mai credenziali: se servono, il fetch automatico si mette in pausa.</p>
            </>
          ) : <p className="muted">Caricamento…</p>}
          {paused && (
            <p role="status">
              In pausa per questo repository ({reasons[status!.reason] ?? 'errore'}): esegui un Fetch manuale per riprenderlo.
            </p>
          )}
        </div>
      )}
    </span>
  );
}
