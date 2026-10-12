import { api } from '../api/client';
import { describeError } from '../api/errors';
import { useApp } from '../store/app';

export function Toasts() {
  const toasts = useApp((s) => s.toasts);
  const dismiss = useApp((s) => s.dismiss);
  const toast = useApp((s) => s.toast);
  const runOp = useApp((s) => s.runOp);
  const act = async (id: number, action: string, tab?: string) => {
    dismiss(id);
    if (!tab) return;
    try {
      if (action === 'pull') await runOp(tab, 'pull', () => api.Pull(tab, ''));
    } catch (e) {
      toast(e, tab);
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
              <button onClick={() => dismiss(t.id)}>Chiudi</button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
