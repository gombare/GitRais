import { useEffect, useId, useRef, useState } from 'react';
import { api } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import type { store } from '../../wailsjs/go/models';

// Bounds of store.Settings.autoFetchSeconds (store.MinAutoFetchSeconds / MaxAutoFetchSeconds).
const MIN_SECONDS = 30;
const MAX_SECONDS = 3600;

// Why the auto-fetch is not running (AutoFetchStatus.reason).
export const AUTOFETCH_REASONS: Record<string, string> = {
  auth: 'in pausa: credenziali',
  network: 'in pausa: rete',
  'waiting-manual': 'si attiva dopo il primo fetch manuale',
};
const explanations: Record<string, string> = {
  auth: 'Il remote ha rifiutato le credenziali: esegui un Fetch manuale per riprenderlo.',
  network: 'Il remote non era raggiungibile: esegui un Fetch manuale per riprenderlo.',
  'waiting-manual': 'Parte dopo il primo Fetch, Pull o Push manuale riuscito in questo repository, così eventuali richieste di credenziali arrivano solo quando le chiedi tu.',
};

// Toolbar control for the periodic background fetch: an on/off switch and the interval
// (global settings), plus whether it is paused for this tab after an auth/network failure.
export function AutoFetchSettings({ tab }: { tab: string }) {
  const toast = useApp((s) => s.toast);
  // A finished remote op (e.g. a manual fetch) may have resumed auto-fetch: re-read the status.
  const op = useApp((s) => !!s.ops[tab]);
  const [open, setOpen] = useState(false);
  const [settings, setSettings] = useState<store.Settings | null>(null);
  const status = useRepo((s) => s.repos[tab]?.autoFetch);
  const setAutoFetch = useRepo((s) => s.setAutoFetch);
  const [seconds, setSeconds] = useState('');
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const id = useId();

  useEffect(() => {
    let live = true;
    // The events keep it live; this read covers the state from before the tab listened.
    api.AutoFetchStatus(tab).then((s) => { if (live && s) setAutoFetch(tab, s); }, () => { /* tab closing: ignore */ });
    return () => { live = false; };
  }, [tab, op, open, setAutoFetch]);

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
      setAutoFetch(tab, await api.AutoFetchStatus(tab));
    } catch (e) {
      toast(e, tab);
    }
  };
  const commitSeconds = () => {
    if (!settings) return;
    const n = Math.min(MAX_SECONDS, Math.max(MIN_SECONDS, Math.round(Number(seconds)) || settings.autoFetchSeconds));
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
  const reason = paused ? (AUTOFETCH_REASONS[status!.reason] ?? 'in pausa') : '';
  const label = status && !status.enabled ? 'Fetch automatico (disattivato)' : reason ? `Fetch automatico (${reason})` : 'Fetch automatico';
  return (
    <span className="af">
      <button ref={button} aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? id : undefined}
        className="af-button" onClick={() => setOpen((o) => !o)} title={reason ? `${label}. ${explanations[status!.reason] ?? ''}` : 'Fetch periodico in background dei remote'}>
        {label}
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
                Intervallo (secondi, da {MIN_SECONDS} a {MAX_SECONDS})
                <input type="number" min={MIN_SECONDS} max={MAX_SECONDS} step={1} value={seconds} disabled={!settings.autoFetch}
                  onChange={(e) => setSeconds(e.target.value)} onBlur={commitSeconds}
                  onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); commitSeconds(); } }} />
              </label>
              <p className="muted">Non chiede mai credenziali: se servono, il fetch automatico si mette in pausa.</p>
            </>
          ) : <p className="muted">Caricamento…</p>}
          {paused && (
            <p role="status">
              Per questo repository: {reason}. {explanations[status!.reason] ?? 'Esegui un Fetch manuale per riprenderlo.'}
            </p>
          )}
        </div>
      )}
    </span>
  );
}
