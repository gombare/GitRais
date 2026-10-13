import { useState } from 'react';
import { api, parseError } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { groupRefs, remoteBranchName } from '../leftpanel/groupRefs';
import { askText, confirmAction } from './Dialogs';
import { createBranchAt, createTagAt, localWrite, stashMenu } from '../actions/repoActions';
import { openMenu, type MenuItem } from './ContextMenu';
import type { gitread, graph } from '../../wailsjs/go/models';

const NO_REFS: gitread.Ref[] = [];
const NO_STASHES: graph.Stash[] = [];

export function LeftPanel({ tab }: { tab: string }) {
  const refs = useRepo((s) => s.repos[tab]?.refs?.refs) ?? NO_REFS;
  const head = useRepo((s) => s.repos[tab]?.refs?.head);
  const stashes = useRepo((s) => s.repos[tab]?.stashes) ?? NO_STASHES;
  const jump = useRepo((s) => s.jump);
  const toast = useApp((s) => s.toast);
  const runOp = useApp((s) => s.runOp);
  const [filter, setFilter] = useState('');
  const [closed, setClosed] = useState<Record<string, boolean>>({});
  const g = groupRefs(refs, filter);

  // Local (non-progress) ops: failures become toasts; Cancelled stays silent.
  const run = (p: Promise<unknown>) => localWrite(tab, p);
  // Ops that can emit op:progress must go through runOp.
  const runRemote = (op: string, fn: () => Promise<unknown>) => runOp(tab, op, fn).catch((e) => toast(e, tab));
  const checkout = (kind: 'local' | 'remote', name: string) => run(api.Checkout(tab, { kind, name }));

  const deleteBranch = async (r: gitread.Ref) => {
    if (!(await confirmAction({ title: 'Elimina branch', message: `Eliminare il branch ${r.short}?`, danger: true }))) return;
    try {
      await useApp.getState().trackWrite(tab, api.DeleteBranch(tab, r.short, false));
    } catch (e) {
      const err = parseError(e);
      // Force only when git refused because the branch is not fully merged, and only after asking again.
      if (/not fully merged/i.test(`${err.message}\n${err.stderr}`)) {
        if (await confirmAction({ title: 'Branch non unito', message: `${r.short} non è completamente unito. Forzare l'eliminazione?`, danger: true })) {
          await run(api.DeleteBranch(tab, r.short, true));
        }
      } else toast(e, tab);
    }
  };

  const localMenu = (r: gitread.Ref): MenuItem[] => [
    { label: `Checkout ${r.short}`, disabled: r.isHead, onClick: () => checkout('local', r.short) },
    { label: 'Crea branch da qui…', onClick: () => createBranchAt(tab, r.name) },
    { label: 'Rinomina…', onClick: async () => {
      const n = await askText({ title: `Rinomina ${r.short}`, label: 'Nuovo nome', initial: r.short });
      if (n && n !== r.short) run(api.RenameBranch(tab, r.short, n));
    } },
    { label: 'Imposta upstream…', onClick: async () => {
      const u = await askText({ title: `Upstream di ${r.short}`, label: 'remote/branch', initial: r.upstream || `origin/${r.short}` });
      if (u) run(api.SetUpstream(tab, r.short, u));
    } },
    { label: 'Crea tag qui…', onClick: () => createTagAt(tab, r.name) },
    'separator',
    { label: `Elimina ${r.short}`, danger: true, disabled: r.isHead, onClick: () => deleteBranch(r) },
  ];

  const remoteMenu = (r: gitread.Ref): MenuItem[] => [
    { label: `Checkout ${r.short}`, onClick: () => checkout('remote', r.short) },
    'separator',
    { label: `Elimina ${r.short} dal remote`, danger: true, onClick: async () => {
      const branch = remoteBranchName(r);
      if (await confirmAction({ title: 'Elimina branch remoto', message: `Eliminare ${branch} da ${r.remote}?`, danger: true })) {
        runRemote('delete-remote-branch', () => api.DeleteRemoteBranch(tab, r.remote, branch));
      }
    } },
  ];

  const remoteNames = Object.keys(groupRefs(refs, '').remotes);
  const tagMenu = (r: gitread.Ref): MenuItem[] => [
    ...(remoteNames.length
      ? remoteNames.map((remote): MenuItem => ({ label: `Push su ${remote}`, onClick: () => runRemote('push-tag', () => api.PushTag(tab, remote, r.short)) }))
      : [{ label: 'Push (nessun remote)', disabled: true, onClick: () => undefined } as MenuItem]),
    'separator',
    { label: `Elimina ${r.short}`, danger: true, onClick: async () => {
      if (await confirmAction({ title: 'Elimina tag', message: `Eliminare il tag ${r.short}?`, danger: true })) run(api.DeleteTag(tab, r.short));
    } },
  ];

  const section = (key: string, title: string, count: number, body: React.ReactNode) => (
    <section key={key}>
      <h4 className="lp-head">
        <button type="button" className="link lp-toggle" aria-expanded={!closed[key]}
          onClick={() => setClosed((c) => ({ ...c, [key]: !c[key] }))}>
          {closed[key] ? '▸' : '▾'} {title} <span className="muted">{count}</span>
        </button>
      </h4>
      {!closed[key] && body}
    </section>
  );

  const row = (key: string, label: string, hash: string, onOpen: () => void, menu: MenuItem[] | null, extra?: React.ReactNode, isHead = false) => (
    <li key={key} tabIndex={0} className={isHead ? 'head' : ''}
      onClick={() => jump(tab, hash)}
      onDoubleClick={onOpen}
      onKeyDown={(e) => { if (e.key === 'Enter') jump(tab, hash); }}
      onContextMenu={menu ? (e) => openMenu(e, menu) : undefined}>
      <span className="lp-name" title={label}>{label}</span>
      {extra}
    </li>
  );
  const refRow = (r: gitread.Ref, menu: MenuItem[], onOpen: () => void) => row(r.name, r.short, r.hash, onOpen, menu, (
    <>
      {(r.ahead > 0 || r.behind > 0) && <span className="muted">↑{r.ahead} ↓{r.behind}</span>}
      {r.gone && <span className="muted">upstream eliminato</span>}
    </>
  ), r.isHead);

  return (
    <div className="left-panel">
      <input className="lp-filter" placeholder="Filtra" aria-label="Filtra riferimenti" value={filter}
        onChange={(e) => setFilter(e.target.value)} />
      {head?.detached && head.hash && (
        <ul>{row('detached', 'HEAD scollegato', head.hash, () => undefined, null, <span className="muted">{head.hash.slice(0, 7)}</span>, true)}</ul>
      )}
      {section('local', 'Locali', g.local.length, <ul>{g.local.map((r) =>
        refRow(r, localMenu(r), () => checkout('local', r.short)))}</ul>)}
      {section('remote', 'Remote', Object.values(g.remotes).reduce((n, l) => n + l.length, 0),
        Object.entries(g.remotes).map(([remote, list]) => (
          <div key={remote}>
            <div className="lp-remote muted">{remote}</div>
            <ul>{list.map((r) => refRow(r, remoteMenu(r), () => checkout('remote', r.short)))}</ul>
          </div>
        )))}
      {section('tags', 'Tag', g.tags.length, <ul>{g.tags.map((r) => refRow(r, tagMenu(r), () => jump(tab, r.hash)))}</ul>)}
      {section('stash', 'Stash', stashes.length, <ul>{stashes.map((s) =>
        row(s.Hash, s.Subject, s.Hash, () => jump(tab, s.Hash), stashMenu(tab, s), <span className="muted">{s.Ref}</span>))}</ul>)}
    </div>
  );
}
