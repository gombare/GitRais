import { api, parseError } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { askText, chooseOption, confirmAction } from './Dialogs';
import { openMenu } from './ContextMenu';

const PULL_MODES = [
  { value: 'merge', label: 'Merge' },
  { value: 'rebase', label: 'Rebase' },
  { value: 'ff-only', label: 'Solo fast-forward' },
];

// Actions that are currently running, keyed by tab and name: a double click
// must not start them twice. Module-level because the toolbar and the
// keyboard shortcuts use separate useRepoActions instances.
const inFlight = new Set<string>();

// Actions shared by the toolbar buttons and the keyboard shortcuts. Remote
// operations go through runOp so their op:progress events reach the toolbar.
export function useRepoActions(tab: string) {
  const toast = useApp((s) => s.toast);
  const busy = () => !!useApp.getState().ops[tab];
  const remote = async (op: string, fn: () => Promise<unknown>) => {
    try {
      await useApp.getState().runOp(tab, op, fn);
    } catch (e) {
      toast(e, tab);
    }
  };
  const once = async (name: string, fn: () => Promise<unknown> | void) => {
    const key = `${tab}:${name}`;
    if (inFlight.has(key)) return;
    inFlight.add(key);
    try {
      await fn();
    } finally {
      inFlight.delete(key);
    }
  };
  const pull = async (mode = ''): Promise<void> => {
    if (busy()) return;
    try {
      await useApp.getState().runOp(tab, 'pull', () => api.Pull(tab, mode as never));
    } catch (e) {
      if (mode === '' && parseError(e).kind === 'PullStrategyRequired') {
        const m = await chooseOption({
          title: 'Come integrare le modifiche?',
          message: 'Le branch locale e remota sono divergenti: scegli la strategia di pull.',
          options: PULL_MODES,
        });
        if (m) await pull(m);
        return;
      }
      toast(e, tab);
    }
  };
  return {
    fetch: () => { if (!busy()) return remote('fetch', () => api.Fetch(tab)); },
    pull,
    push: (force = false) => { if (!busy()) return remote('push', () => api.Push(tab, force)); },
    forcePush: async () => {
      const ok = await confirmAction({
        title: 'Force push',
        message: 'Il branch remoto verrà sovrascritto con quello locale (force-with-lease: fallisce se il remote è cambiato nel frattempo). Continuare?',
        danger: true,
      });
      if (ok && !busy()) await remote('push', () => api.Push(tab, true));
    },
    branch: () => once('branch', async () => {
      const n = await askText({ title: 'Nuovo branch da HEAD', label: 'Nome' });
      if (n) await api.CreateBranch(tab, n, 'HEAD', true).catch((e) => toast(e, tab));
    }),
    stash: () => once('stash', () => api.StashPush(tab, '', true).catch((e) => toast(e, tab))),
    pop: () => {
      // The newest stash comes first; the backend identifies stashes by commit hash.
      const top = useRepo.getState().repos[tab]?.stashes[0];
      if (!top) return;
      return api.StashPop(tab, top.Hash).catch((e) => toast(e, tab));
    },
  };
}

export function Toolbar({ tab }: { tab: string }) {
  const op = useApp((s) => s.ops[tab]);
  const toast = useApp((s) => s.toast);
  const stashes = useRepo((s) => s.repos[tab]?.stashes.length ?? 0);
  const head = useRepo((s) => s.repos[tab]?.refs?.head);
  const a = useRepoActions(tab);
  const busy = !!op;
  return (
    <div className="toolbar" role="toolbar" aria-label="Operazioni repository">
      <button disabled title="Disponibile in una versione successiva">Annulla modifica</button>
      <button disabled title="Disponibile in una versione successiva">Ripeti</button>
      <span className="tb-sep" />
      <button disabled={busy} onClick={a.fetch} title="Fetch (Cmd+Shift+F)">Fetch</button>
      <span className="tb-group">
        <button disabled={busy} onClick={() => a.pull()}>Pull</button>
        <button disabled={busy} className="tb-more" aria-label="Altre opzioni pull" aria-haspopup="menu"
          onClick={(e) => openMenu(e, PULL_MODES.map((m) => ({ label: `Pull (${m.label.toLowerCase()})`, onClick: () => a.pull(m.value) })))}
          onContextMenu={(e) => openMenu(e, PULL_MODES.map((m) => ({ label: `Pull (${m.label.toLowerCase()})`, onClick: () => a.pull(m.value) })))}>▾</button>
      </span>
      <span className="tb-group">
        <button disabled={busy} onClick={() => a.push()}>Push</button>
        <button disabled={busy} className="tb-more" aria-label="Altre opzioni push" aria-haspopup="menu"
          onClick={(e) => openMenu(e, [{ label: 'Force push (with lease)…', danger: true, onClick: a.forcePush }])}
          onContextMenu={(e) => openMenu(e, [{ label: 'Force push (with lease)…', danger: true, onClick: a.forcePush }])}>▾</button>
      </span>
      <span className="tb-sep" />
      <button onClick={a.branch} title="Nuovo branch (Cmd+B)">Branch</button>
      <button onClick={a.stash}>Stash</button>
      <button disabled={stashes === 0} aria-label="Pop stash" onClick={a.pop}>Pop</button>
      <span className="tb-flex" />
      <span className="muted">{head ? (head.detached ? `HEAD staccata ${head.hash.slice(0, 7)}` : head.branch) : ''}</span>
      {op && (
        <span className="tb-op" role="status">
          {op.op}… {op.phase} {op.percent > 0 && `${op.percent}%`}
          <button aria-label="Annulla operazione"
            onClick={() => api.CancelOp(tab).catch((e) => toast(e, tab))}>Annulla</button>
        </span>
      )}
    </div>
  );
}
