import { api, parseError } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { askText, askTextCheck, chooseOption, confirmAction } from '../components/Dialogs';
import type { MenuItem } from '../components/ContextMenu';
import { remoteBranchName } from '../leftpanel/groupRefs';
import type { gitread, graph } from '../../wailsjs/go/models';

// Repository actions shared by several surfaces (toolbar, toasts, left panel, graph, file lists).

export const PULL_MODES = [
  { value: 'merge', label: 'Merge' },
  { value: 'rebase', label: 'Rebase' },
  { value: 'ff-only', label: 'Solo fast-forward' },
];

const toast = (e: unknown, tab: string) => useApp.getState().toast(e, tab);
const uiError = (kind: string) => JSON.stringify({ kind, message: '', stderr: '' });
const success = (tab: string, title: string, detail = '', timeout?: number) =>
  useApp.getState().notify({ severity: 'success', title, detail, tab, timeout });
// A copy confirmation needs only a glance.
const COPIED_TOAST_MS = 2000;

// Runs a local write binding: the toolbar shows "operation running" while it is in flight,
// and a failure becomes a toast (Cancelled stays silent). Never rejects.
export function localWrite(tab: string, p: Promise<unknown>): Promise<void> {
  return useApp.getState().trackWrite(tab, p).then(() => undefined, (e) => toast(e, tab));
}

// Runs a progress-emitting (remote) binding through runOp. Success shows `done` as a short
// success notice; a failure becomes a toast. Refused (OpInProgress notice) while another remote
// op runs on the tab. Resolves true on success. Never rejects.
export async function remoteOp(tab: string, op: string, fn: () => Promise<unknown>, done?: { title: string; detail?: string }): Promise<boolean> {
  const app = useApp.getState();
  if (app.ops[tab]) {
    toast(uiError('OpInProgress'), tab);
    return false;
  }
  try {
    await app.runOp(tab, op, fn);
  } catch (e) {
    toast(e, tab);
    return false;
  }
  if (done) success(tab, done.title, done.detail);
  return true;
}

export const fetchAll = (tab: string) =>
  remoteOp(tab, 'fetch', () => api.Fetch(tab), { title: 'Fetch completato', detail: 'Riferimenti remoti aggiornati.' });

export const push = (tab: string, force = false) =>
  remoteOp(tab, 'push', () => api.Push(tab, force), { title: force ? 'Force push completato' : 'Push completato' });

// Pull through runOp (progress events). With no mode, a PullStrategyRequired failure asks
// for merge/rebase/ff-only and retries with the choice. A DirtyWorktree failure (local
// changes the pull would overwrite) offers an opt-in autostash pull with the same mode:
// never the default, and never for UntrackedWouldBeOverwritten (stashing would not help).
// Other failures become toasts. Ignored while another remote op runs on the tab.
export async function pull(tab: string, mode = ''): Promise<void> {
  const app = useApp.getState();
  if (app.ops[tab]) {
    toast(uiError('OpInProgress'), tab);
    return;
  }
  try {
    await app.runOp(tab, 'pull', () => api.Pull(tab, mode as never));
    success(tab, 'Pull completato');
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
      if (c === 'autostash') await remoteOp(tab, 'pull', () => api.PullAutostash(tab, mode as never),
        { title: 'Pull completato', detail: 'Le modifiche locali sono state riapplicate.' });
      return;
    }
    toast(e, tab);
  }
}

// Copies text to the clipboard with a short confirmation; `what` names it ("Hash abc1234").
export function copyText(tab: string, text: string, what: string): Promise<void> {
  if (!navigator.clipboard) {
    toast(uiError('ClipboardUnavailable'), tab);
    return Promise.resolve();
  }
  return navigator.clipboard.writeText(text).then(
    () => success(tab, 'Copiato ✓', `${what} negli appunti.`, COPIED_TOAST_MS),
    (e) => toast(e, tab),
  );
}

// Shows a working-tree path (relative to the repository root) in the Finder.
export function revealInFinder(tab: string, path: string): Promise<void> {
  return api.RevealInFinder(tab, path).catch((e) => toast(e, tab));
}

// Asks for a branch name (and whether to check it out), then creates it at `at`.
export async function createBranchAt(tab: string, at: string, title = 'Nuovo branch', initial = '') {
  const r = await askTextCheck({ title, label: 'Nome', initial, confirmLabel: 'Crea branch', check: { label: 'Checkout del nuovo branch', initial: true } });
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

// Detached checkout of a commit (or a tag's commit), after a warning.
async function checkoutDetached(tab: string, hash: string, what: string) {
  const ok = await confirmAction({
    title: `Checkout di ${what}`,
    message: `Il checkout di ${what} lascia HEAD staccata (detached HEAD): i nuovi commit non apparterranno a nessun branch finché non ne crei uno. Continuare?`,
    confirmLabel: 'Checkout',
  });
  if (ok) await localWrite(tab, api.Checkout(tab, { kind: 'commit', name: hash }));
}

// Remote names, from the remote-tracking branches.
export function remoteNames(refs: gitread.Ref[]): string[] {
  return [...new Set(refs.filter((r) => r.kind === 'remote' && r.remote).map((r) => r.remote))].sort();
}

// Splits an upstream such as "origin/feat/x" into remote and branch, matching the longest
// known remote name (remote names may contain "/"). null when no known remote matches.
export function splitUpstream(upstream: string, remotes: string[]): { remote: string; branch: string } | null {
  const remote = remotes.filter((r) => upstream.startsWith(`${r}/`)).sort((a, b) => b.length - a.length)[0];
  return remote ? { remote, branch: upstream.slice(remote.length + 1) } : null;
}

// Deletes a local branch; when git refuses because it is not fully merged, asks again before
// forcing. Resolves true when the branch was deleted.
export async function deleteLocalBranch(tab: string, name: string): Promise<boolean> {
  try {
    await useApp.getState().trackWrite(tab, api.DeleteBranch(tab, name, false));
    return true;
  } catch (e) {
    const err = parseError(e);
    if (!/not fully merged/i.test(`${err.message}\n${err.stderr}`)) {
      toast(e, tab);
      return false;
    }
    if (!(await confirmAction({ title: 'Branch non unito', message: `${name} non è completamente unito. Forzare l'eliminazione?`, danger: true, confirmLabel: 'Forza eliminazione' }))) return false;
    try {
      await useApp.getState().trackWrite(tab, api.DeleteBranch(tab, name, true));
      return true;
    } catch (e2) {
      toast(e2, tab);
      return false;
    }
  }
}

const deleteRemote = (tab: string, remote: string, branch: string) =>
  remoteOp(tab, 'delete-remote-branch', () => api.DeleteRemoteBranch(tab, remote, branch),
    { title: 'Branch remoto eliminato', detail: `${branch} eliminato da ${remote}.` });

// Context menu of a local branch. Pull/Push only for the current branch: the backend pushes
// the current branch only (no binding pushes another one), and unsetting an upstream is not
// supported either.
export function localBranchMenu(tab: string, r: gitread.Ref, remotes: string[]): MenuItem[] {
  const up = r.upstream && !r.gone ? splitUpstream(r.upstream, remotes) : null;
  return [
    { label: `Checkout ${r.short}`, disabled: r.isHead, title: r.isHead ? 'È già il branch corrente' : undefined,
      onClick: () => localWrite(tab, api.Checkout(tab, { kind: 'local', name: r.short })) },
    'separator',
    ...(r.isHead ? [
      { label: 'Pull', onClick: () => pull(tab) },
      { label: r.upstream ? 'Push' : 'Push (imposta upstream)', onClick: () => push(tab) },
    ] as MenuItem[] : []),
    { label: 'Imposta upstream…', onClick: async () => {
      const u = await askText({ title: `Upstream di ${r.short}`, label: 'remote/branch', initial: r.upstream || `${remotes[0] ?? 'origin'}/${r.short}`, confirmLabel: 'Imposta upstream' });
      if (u) await localWrite(tab, api.SetUpstream(tab, r.short, u));
    } },
    'separator',
    { label: 'Crea branch da qui…', onClick: () => createBranchAt(tab, r.name) },
    { label: 'Crea tag qui…', onClick: () => createTagAt(tab, r.name) },
    { label: 'Rinomina…', onClick: async () => {
      const n = await askText({ title: `Rinomina ${r.short}`, label: 'Nuovo nome', initial: r.short, confirmLabel: 'Rinomina' });
      if (n && n !== r.short) await localWrite(tab, api.RenameBranch(tab, r.short, n));
    } },
    'separator',
    { label: 'Copia nome del branch', onClick: () => copyText(tab, r.short, `Branch ${r.short}`) },
    'separator',
    { label: `Elimina ${r.short}`, danger: true, disabled: r.isHead, title: r.isHead ? 'Non puoi eliminare il branch corrente: fai prima checkout di un altro' : undefined,
      onClick: async () => {
        if (await confirmAction({ title: 'Elimina branch', message: `Eliminare il branch ${r.short}?`, danger: true, confirmLabel: 'Elimina branch' })) {
          await deleteLocalBranch(tab, r.short);
        }
      } },
    ...(up ? [{
      label: `Elimina ${r.short} anche su ${up.remote}…`, danger: true, disabled: r.isHead,
      title: r.isHead ? 'Non puoi eliminare il branch corrente: fai prima checkout di un altro' : undefined,
      onClick: async () => {
        const ok = await confirmAction({
          title: 'Elimina branch locale e remoto',
          message: `Eliminare il branch locale ${r.short} e il branch ${up.branch} su ${up.remote}? Il branch remoto sparisce anche per gli altri.`,
          items: [r.short, `${up.remote}/${up.branch}`],
          danger: true,
          confirmLabel: 'Elimina entrambi',
        });
        if (ok && await deleteLocalBranch(tab, r.short)) await deleteRemote(tab, up.remote, up.branch);
      },
    } as MenuItem] : []),
  ];
}

// Context menu of a remote-tracking branch. Checkout creates the local branch, or switches to
// the existing one and fast-forwards it when strictly behind (backend).
export function remoteBranchMenu(tab: string, r: gitread.Ref): MenuItem[] {
  const branch = remoteBranchName(r);
  return [
    { label: `Checkout ${r.short}`, onClick: () => localWrite(tab, api.Checkout(tab, { kind: 'remote', name: r.short })) },
    { label: 'Crea branch locale da qui…', onClick: () => createBranchAt(tab, r.name, `Nuovo branch da ${r.short}`, branch) },
    'separator',
    { label: 'Copia nome del branch', onClick: () => copyText(tab, r.short, `Branch ${r.short}`) },
    'separator',
    { label: `Elimina ${r.short} dal remote`, danger: true, onClick: async () => {
      if (await confirmAction({ title: 'Elimina branch remoto', message: `Eliminare ${branch} da ${r.remote}? Il branch sparisce anche per gli altri.`, danger: true, confirmLabel: 'Elimina dal remote' })) {
        await deleteRemote(tab, r.remote, branch);
      }
    } },
  ];
}

// Context menu of a tag. Deleting a tag on a remote has no backend binding: local delete only.
export function tagMenu(tab: string, r: gitread.Ref, remotes: string[]): MenuItem[] {
  return [
    { label: `Checkout ${r.short}…`, onClick: () => checkoutDetached(tab, r.hash, `${r.short}`) },
    'separator',
    ...(remotes.length
      ? remotes.map((remote): MenuItem => ({ label: `Push su ${remote}`, onClick: () => remoteOp(tab, 'push-tag', () => api.PushTag(tab, remote, r.short), { title: 'Tag inviato', detail: `${r.short} inviato a ${remote}.` }) }))
      : [{ label: 'Push', disabled: true, title: 'Il repository non ha remote', onClick: () => undefined } as MenuItem]),
    'separator',
    { label: 'Copia nome del tag', onClick: () => copyText(tab, r.short, `Tag ${r.short}`) },
    'separator',
    { label: `Elimina ${r.short}`, danger: true, onClick: async () => {
      if (await confirmAction({ title: 'Elimina tag', message: `Eliminare il tag locale ${r.short}? Sui remote resta.`, danger: true, confirmLabel: 'Elimina tag' })) {
        await localWrite(tab, api.DeleteTag(tab, r.short));
      }
    } },
  ];
}

// The full commit message (summary and body); the subject alone when it cannot be read.
async function commitMessage(tab: string, hash: string, subject: string): Promise<string> {
  try {
    const d = await api.CommitDetail(tab, hash);
    return d.body ? `${d.summary}\n\n${d.body}` : d.summary;
  } catch {
    return subject;
  }
}

// Context menu of a commit row in the graph.
export function commitMenu(tab: string, hash: string, subject = ''): MenuItem[] {
  return [
    { label: 'Mostra modifiche', onClick: () => useRepo.getState().select(tab, { kind: 'commit', hash }) },
    'separator',
    { label: 'Checkout di questo commit', onClick: () => checkoutDetached(tab, hash, hash.slice(0, 7)) },
    { label: 'Crea branch qui…', onClick: () => createBranchAt(tab, hash) },
    { label: 'Crea tag qui…', onClick: () => createTagAt(tab, hash) },
    'separator',
    { label: 'Copia hash', onClick: () => copyText(tab, hash, `Hash ${hash.slice(0, 10)}`) },
    { label: 'Copia hash breve', onClick: () => copyText(tab, hash.slice(0, 7), `Hash ${hash.slice(0, 7)}`) },
    { label: 'Copia messaggio', onClick: async () => copyText(tab, await commitMessage(tab, hash, subject), 'Messaggio del commit') },
  ];
}

// Context menu of a stash (left panel and graph). Operations take the stash commit HASH,
// never stash@{N}: indexes shift as stashes change.
export function stashMenu(tab: string, s: Pick<graph.Stash, 'Hash' | 'Ref' | 'Subject'>): MenuItem[] {
  return [
    { label: 'Applica', onClick: () => localWrite(tab, api.StashApply(tab, s.Hash)) },
    { label: 'Pop', onClick: () => localWrite(tab, api.StashPop(tab, s.Hash)) },
    'separator',
    { label: 'Copia messaggio', onClick: () => copyText(tab, s.Subject, 'Messaggio dello stash') },
    'separator',
    { label: 'Elimina', danger: true, onClick: async () => {
      if (await confirmAction({ title: 'Elimina stash', message: `Eliminare ${s.Ref} (${s.Subject})?`, danger: true, confirmLabel: 'Elimina stash' })) {
        await localWrite(tab, api.StashDrop(tab, s.Hash));
      }
    } },
  ];
}

// Items shared by every file row: copy the path, show the file in the Finder.
export function fileItems(tab: string, path: string): MenuItem[] {
  return [
    { label: 'Copia percorso', onClick: () => copyText(tab, path, `Percorso ${path}`) },
    { label: 'Mostra nel Finder', onClick: () => revealInFinder(tab, path) },
  ];
}
