import { useRef } from 'react';
import { api } from '../api/client';
import { describeError } from '../api/errors';
import { useApp } from '../store/app';
import { confirmAction } from './Dialogs';
import { pull } from '../actions/repoActions';

export function Toasts() {
  const toasts = useApp((s) => s.toasts);
  const dismiss = useApp((s) => s.dismiss);
  const toast = useApp((s) => s.toast);
  // Actions already running, so a double click cannot start them twice.
  const running = useRef(new Set<string>());
  const act = async (id: number, action: string, tab?: string) => {
    const key = `${tab}:${action}`;
    if (running.current.has(key)) return;
    running.current.add(key);
    dismiss(id);
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
        });
        if (ok) {
          await useApp.getState().trackWrite(tab, api.RemoveIndexLock(tab));
          toast(JSON.stringify({ kind: 'LockRemoved', message: 'Lock rimosso', stderr: '' }), tab);
        }
      }
    } catch (e) {
      toast(e, tab);
    } finally {
      running.current.delete(key);
    }
  };
  return (
    <div className="toasts">
      {toasts.map((t) => {
        const d = describeError(t.error);
        return (
          <div key={t.id} className="toast" role="alert">
            <strong>{d.title}</strong>
            <pre>{d.detail}</pre>
            <div className="toast-actions">
              {d.action === 'pull' && t.tab && <button onClick={() => act(t.id, 'pull', t.tab)}>Pull</button>}
              {d.action === 'removeLock' && t.tab && <button onClick={() => act(t.id, 'removeLock', t.tab)}>Rimuovi lock</button>}
              <button onClick={() => dismiss(t.id)}>Chiudi</button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
