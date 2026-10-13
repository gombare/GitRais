import { useEffect, useRef, useState } from 'react';
import { api } from '../api/client';
import type { Severity } from '../api/errors';
import { toastTimeout, useApp, type Toast } from '../store/app';
import { confirmAction } from './Dialogs';
import { pull } from '../actions/repoActions';

// Older toasts beyond this are collapsed into a "+N" button.
const MAX_VISIBLE = 3;

const ICONS: Record<Severity, string> = { error: '×', warning: '!', success: '✓', info: 'i' };
// Read by screen readers before the title (the icon itself is decorative).
const LABELS: Record<Severity, string> = { error: 'Errore', warning: 'Attenzione', success: 'Completato', info: 'Informazione' };

function ToastItem({ t, onAction }: { t: Toast; onAction: (t: Toast, action: string) => void }) {
  const dismiss = useApp((s) => s.dismiss);
  const ms = toastTimeout(t);
  // The countdown restarts from scratch once neither hover nor focus holds it.
  const [hover, setHover] = useState(false);
  const [focus, setFocus] = useState(false);
  useEffect(() => {
    if (!ms || hover || focus) return;
    const id = window.setTimeout(() => dismiss(t.id), ms);
    return () => window.clearTimeout(id);
  }, [ms, hover, focus, t.id, dismiss]);
  return (
    <div className={`toast toast-${t.severity}`} role={t.severity === 'error' ? 'alert' : 'status'}
      onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}
      onFocus={() => setFocus(true)}
      onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setFocus(false); }}>
      <span className="toast-icon" aria-hidden="true">{ICONS[t.severity]}</span>
      <div className="toast-body">
        <strong className="toast-title"><span className="sr-only">{LABELS[t.severity]}: </span>{t.title}</strong>
        {t.detail && <p className="toast-detail">{t.detail}</p>}
        {t.stderr && (
          <details className="toast-stderr">
            <summary>Dettagli</summary>
            <pre>{t.stderr}</pre>
          </details>
        )}
        <div className="toast-actions">
          {t.action === 'pull' && t.tab && <button onClick={() => onAction(t, 'pull')}>Pull</button>}
          {t.action === 'removeLock' && t.tab && <button onClick={() => onAction(t, 'removeLock')}>Rimuovi lock</button>}
          <button onClick={() => dismiss(t.id)}>Chiudi</button>
        </div>
      </div>
    </div>
  );
}

export function Toasts() {
  const all = useApp((s) => s.toasts);
  const active = useApp((s) => s.active);
  const dismiss = useApp((s) => s.dismiss);
  const toast = useApp((s) => s.toast);
  const notify = useApp((s) => s.notify);
  const [expanded, setExpanded] = useState(false);
  // Actions already running, so a double click cannot start them twice.
  const running = useRef(new Set<string>());
  // Tab-scoped toasts show only on their tab; global ones (no tab) everywhere, Welcome included.
  const toasts = all.filter((t) => !t.tab || t.tab === active);
  const overflow = toasts.length > MAX_VISIBLE;
  useEffect(() => { if (!overflow) setExpanded(false); }, [overflow]);
  const hidden = overflow && !expanded ? toasts.length - MAX_VISIBLE : 0;

  const act = async (t: Toast, action: string) => {
    const tab = t.tab;
    const key = `${tab}:${action}`;
    if (running.current.has(key)) return;
    running.current.add(key);
    dismiss(t.id);
    if (!tab) {
      running.current.delete(key);
      return;
    }
    try {
      // Same flow as the toolbar button, including the merge/rebase/ff-only choice.
      if (action === 'pull') await pull(tab);
      if (action === 'removeLock') {
        // The backend only removes <gitdir>/index.lock, and only if no process holds it.
        const ok = await confirmAction({
          title: 'Rimuovere index.lock?',
          message: 'Verifica che nessun altro programma stia usando git su questo repository. Il file viene rimosso solo se nessun processo lo tiene aperto.',
          danger: true,
          confirmLabel: 'Rimuovi lock',
        });
        if (ok) {
          await useApp.getState().trackWrite(tab, api.RemoveIndexLock(tab));
          notify({ severity: 'success', title: 'Lock rimosso', detail: 'index.lock è stato rimosso: riprova l’operazione.', tab });
        }
      }
    } catch (e) {
      toast(e, tab);
    } finally {
      running.current.delete(key);
    }
  };

  if (!toasts.length) return null;
  return (
    <section className="toasts" aria-label="Notifiche">
      {hidden > 0 && (
        <button className="toast-more" onClick={() => setExpanded(true)} aria-label={`Mostra altre ${hidden} notifiche`}>+{hidden}</button>
      )}
      {overflow && expanded && <button className="toast-more" onClick={() => setExpanded(false)}>Comprimi</button>}
      {toasts.slice(hidden).map((t) => <ToastItem key={t.id} t={t} onAction={act} />)}
    </section>
  );
}
