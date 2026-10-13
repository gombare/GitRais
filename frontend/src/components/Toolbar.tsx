import { api } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { askTextCheck, confirmAction } from './Dialogs';
import { openMenu } from './ContextMenu';
import { createBranchAt, localWrite, pull, PULL_MODES } from '../actions/repoActions';

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
  return {
    fetch: () => { if (!busy()) return remote('fetch', () => api.Fetch(tab)); },
    pull: (mode = '') => pull(tab, mode),
    push: (force = false) => { if (!busy()) return remote('push', () => api.Push(tab, force)); },
    forcePush: async () => {
      const ok = await confirmAction({
        title: 'Force push',
        message: 'Il branch remoto verrà sovrascritto con quello locale (force-with-lease: fallisce se il remote è cambiato nel frattempo). Continuare?',
        danger: true,
      });
      if (ok && !busy()) await remote('push', () => api.Push(tab, true));
    },
    branch: () => once('branch', () => createBranchAt(tab, 'HEAD', 'Nuovo branch da HEAD')),
    // Optional message; untracked files are included unless the user unticks the box.
    stash: () => once('stash', async () => {
      const r = await askTextCheck({
        title: 'Stash delle modifiche', label: 'Messaggio (facoltativo)', allowEmpty: true,
        check: { label: 'Includi i file non tracciati', initial: true },
      });
      if (r) await localWrite(tab, api.StashPush(tab, r.text, r.checked));
    }),
    pop: () => {
      // The newest stash comes first; the backend identifies stashes by commit hash.
      const top = useRepo.getState().repos[tab]?.stashes[0];
      if (!top) return;
      return localWrite(tab, api.StashPop(tab, top.Hash));
    },
  };
}

export function Toolbar({ tab }: { tab: string }) {
  const op = useApp((s) => s.ops[tab]);
  const writing = useApp((s) => (s.writes[tab] ?? 0) > 0);
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
      {/* Always mounted so screen readers announce changes; a remote op shows its own progress instead. */}
      <span className="tb-busy" aria-live="polite">{writing && !op ? 'Operazione in corso…' : ''}</span>
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
