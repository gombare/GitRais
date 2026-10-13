import { api, parseError } from '../api/client';
import { useApp } from '../store/app';
import { askText, askTextCheck, chooseOption, confirmAction } from '../components/Dialogs';
import type { MenuItem } from '../components/ContextMenu';
import type { graph } from '../../wailsjs/go/models';

// Repository actions shared by several surfaces (toolbar, toasts, left panel, graph).

export const PULL_MODES = [
  { value: 'merge', label: 'Merge' },
  { value: 'rebase', label: 'Rebase' },
  { value: 'ff-only', label: 'Solo fast-forward' },
];

const toast = (e: unknown, tab: string) => useApp.getState().toast(e, tab);
// A copy confirmation needs only a glance.
const COPIED_TOAST_MS = 2000;

// Runs a local write binding: the toolbar shows "operation running" while it is in flight,
// and a failure becomes a toast (Cancelled stays silent). Never rejects.
export function localWrite(tab: string, p: Promise<unknown>): Promise<void> {
  return useApp.getState().trackWrite(tab, p).then(() => undefined, (e) => toast(e, tab));
}

// Pull through runOp (progress events). With no mode, a PullStrategyRequired failure asks
// for merge/rebase/ff-only and retries with the choice. A DirtyWorktree failure (local
// changes the pull would overwrite) offers an opt-in autostash pull with the same mode:
// never the default. Other failures become toasts. Ignored while another remote op runs on the tab.
export async function pull(tab: string, mode = ''): Promise<void> {
  const app = useApp.getState();
  if (app.ops[tab]) {
    toast(JSON.stringify({ kind: 'OpInProgress', message: '', stderr: '' }), tab);
    return;
  }
  try {
    await app.runOp(tab, 'pull', () => api.Pull(tab, mode as never));
  } catch (e) {
    const kind = parseError(e).kind;
    if (mode === '' && kind === 'PullStrategyRequired') {
      const m = await chooseOption({
        title: 'Come integrare le modifiche?',
        message: 'Le branch locale e remota sono divergenti: scegli la strategia di pull.',
        options: PULL_MODES,
      });
      if (m) await pull(tab, m);
      return;
    }
    if (kind === 'DirtyWorktree') {
      const c = await chooseOption({
        title: 'Modifiche locali',
        message: 'Il pull sovrascriverebbe modifiche locali non committate. Puoi metterle da parte, fare pull e riapplicarle: in caso di conflitti restano nella lista degli stash.',
        options: [{ value: 'autostash', label: 'Metti da parte le modifiche, fai pull e riapplica' }],
      });
      if (c === 'autostash') await pullAutostash(tab, mode);
      return;
    }
    toast(e, tab);
  }
}

// Pull with autostash through runOp; failures (AutostashConflict included) become toasts.
async function pullAutostash(tab: string, mode: string): Promise<void> {
  const app = useApp.getState();
  if (app.ops[tab]) {
    toast(JSON.stringify({ kind: 'OpInProgress', message: '', stderr: '' }), tab);
    return;
  }
  try {
    await app.runOp(tab, 'pull', () => api.PullAutostash(tab, mode as never));
  } catch (e) {
    toast(e, tab);
  }
}

// Asks for a branch name (and whether to check it out), then creates it at `at`.
export async function createBranchAt(tab: string, at: string, title = 'Nuovo branch') {
  const r = await askTextCheck({ title, label: 'Nome', confirmLabel: 'Crea branch', check: { label: 'Checkout del nuovo branch', initial: true } });
  if (r) await localWrite(tab, api.CreateBranch(tab, r.text, at, r.checked));
}

// Asks for a tag name and an optional message: with a message the tag is annotated.
export async function createTagAt(tab: string, at: string) {
  const name = await askText({ title: 'Nuovo tag', label: 'Nome', confirmLabel: 'Avanti' });
  if (!name) return;
  const message = await askText({ title: `Tag ${name}`, label: 'Messaggio (facoltativo: se presente il tag è annotato)', allowEmpty: true, confirmLabel: 'Crea tag' });
  if (message === null) return;
  await localWrite(tab, api.CreateTag(tab, name, at, message));
}

// Context menu of a commit row in the graph.
export function commitMenu(tab: string, hash: string): MenuItem[] {
  return [
    { label: 'Checkout di questo commit', onClick: async () => {
      const ok = await confirmAction({
        title: 'Checkout del commit',
        message: `Il checkout di ${hash.slice(0, 7)} lascia HEAD staccata (detached HEAD): i nuovi commit non apparterranno a nessun branch finché non ne crei uno. Continuare?`,
        confirmLabel: 'Checkout',
      });
      if (ok) await localWrite(tab, api.Checkout(tab, { kind: 'commit', name: hash }));
    } },
    { label: 'Crea branch qui…', onClick: () => createBranchAt(tab, hash) },
    { label: 'Crea tag qui…', onClick: () => createTagAt(tab, hash) },
    'separator',
    { label: 'Copia hash', onClick: () => {
      if (!navigator.clipboard) toast(JSON.stringify({ kind: 'ClipboardUnavailable', message: '', stderr: '' }), tab);
      else navigator.clipboard.writeText(hash).then(
        () => useApp.getState().notify({ severity: 'success', title: 'Copiato ✓', detail: `Hash ${hash.slice(0, 10)} negli appunti.`, tab, timeout: COPIED_TOAST_MS }),
        (e) => toast(e, tab),
      );
    } },
  ];
}

// Context menu of a stash (left panel and graph). Operations take the stash commit HASH,
// never stash@{N}: indexes shift as stashes change.
export function stashMenu(tab: string, s: Pick<graph.Stash, 'Hash' | 'Ref' | 'Subject'>): MenuItem[] {
  return [
    { label: 'Applica', onClick: () => localWrite(tab, api.StashApply(tab, s.Hash)) },
    { label: 'Pop', onClick: () => localWrite(tab, api.StashPop(tab, s.Hash)) },
    'separator',
    { label: 'Elimina', danger: true, onClick: async () => {
      if (await confirmAction({ title: 'Elimina stash', message: `Eliminare ${s.Ref} (${s.Subject})?`, danger: true, confirmLabel: 'Elimina stash' })) {
        await localWrite(tab, api.StashDrop(tab, s.Hash));
      }
    } },
  ];
}
