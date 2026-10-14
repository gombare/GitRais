import { useState } from 'react';
import { api } from '../api/client';
import { useRepo } from '../store/repo';
import { groupRefs } from '../leftpanel/groupRefs';
import { localBranchMenu, localWrite, remoteBranchMenu, remoteNames, stashMenu, tagMenu } from '../actions/repoActions';
import { openMenu, openMenuAt, type MenuItem } from './ContextMenu';
import type { gitread, graph } from '../../wailsjs/go/models';

const NO_REFS: gitread.Ref[] = [];

// Arrow-and-count badge; screen readers get the spelled-out text instead of the arrow.
const badge = (kind: 'ahead' | 'behind', short: string, long: string) => (
  <span className={`badge badge-${kind}`} title={long}>
    <span aria-hidden="true">{short}</span><span className="sr-only">{long}</span>
  </span>
);
const NO_STASHES: graph.Stash[] = [];

export function LeftPanel({ tab }: { tab: string }) {
  const refs = useRepo((s) => s.repos[tab]?.refs?.refs) ?? NO_REFS;
  const head = useRepo((s) => s.repos[tab]?.refs?.head);
  const stashes = useRepo((s) => s.repos[tab]?.stashes) ?? NO_STASHES;
  const jump = useRepo((s) => s.jump);
  const [filter, setFilter] = useState('');
  const [closed, setClosed] = useState<Record<string, boolean>>({});
  const g = groupRefs(refs, filter);

  const remotes = remoteNames(refs);
  const checkout = (kind: 'local' | 'remote', name: string) => localWrite(tab, api.Checkout(tab, { kind, name }));

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
      onKeyDown={(e) => {
        if (e.key === 'Enter') jump(tab, hash);
        // WebKit does not always turn these keys into a contextmenu event on a list item.
        if (menu && (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey))) {
          e.preventDefault();
          openMenuAt(e.currentTarget, menu);
        }
      }}
      onContextMenu={menu ? (e) => openMenu(e, menu) : undefined}>
      <span className="lp-name" title={label}>{label}</span>
      {extra}
    </li>
  );
  const refRow = (r: gitread.Ref, menu: MenuItem[], onOpen: () => void) => row(r.name, r.short, r.hash, onOpen, menu, (
    <>
      {r.ahead > 0 && badge('ahead', `↑${r.ahead}`, `${r.ahead} commit da inviare`)}
      {r.behind > 0 && badge('behind', `↓${r.behind}`, `${r.behind} commit da ricevere`)}
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
        refRow(r, localBranchMenu(tab, r, remotes), () => checkout('local', r.short)))}</ul>)}
      {section('remote', 'Remote', Object.values(g.remotes).reduce((n, l) => n + l.length, 0),
        Object.entries(g.remotes).map(([remote, list]) => (
          <div key={remote}>
            <div className="lp-remote muted">{remote}</div>
            <ul>{list.map((r) => refRow(r, remoteBranchMenu(tab, r), () => checkout('remote', r.short)))}</ul>
          </div>
        )))}
      {section('tags', 'Tag', g.tags.length, <ul>{g.tags.map((r) => refRow(r, tagMenu(tab, r, remotes), () => jump(tab, r.hash)))}</ul>)}
      {section('stash', 'Stash', stashes.length, <ul>{stashes.map((s) =>
        row(s.Hash, s.Subject, s.Hash, () => jump(tab, s.Hash), stashMenu(tab, s), <span className="muted">{s.Ref}</span>))}</ul>)}
    </div>
  );
}
