import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { api } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { useKeybindings } from '../hooks/useKeybindings';
import { GraphView, type GraphHandle } from './GraphView';
import { LeftPanel } from './LeftPanel';
import { DetailPanel } from './DetailPanel';
import { Resizer } from './Resizer';
import { Toolbar, useRepoActions } from './Toolbar';
import type { store } from '../../wailsjs/go/models';

// Monaco is large: the diff viewer is a separate chunk, loaded on the first diff.
const DiffView = lazy(() => import('./DiffView'));

const LEFT = { min: 180, max: 600, def: 260 };
const RIGHT = { min: 260, max: 700, def: 340 };
const clamp = (v: number, r: { min: number; max: number }) => Math.max(r.min, Math.min(r.max, v));

// Paths of tabs closed with the keyboard, most recent last (view:reopenTab).
const closedTabs: string[] = [];

export function RepoView({ tab }: { tab: string }) {
  const root = useRef<HTMLDivElement>(null);
  const graph = useRef<GraphHandle>(null);
  const active = useApp((s) => s.active === tab);
  const saved = useApp((s) => s.state?.layout);
  const [layout, setLayout] = useState({ leftWidth: LEFT.def, rightWidth: RIGHT.def });
  // Latest layout for the persist callbacks (a drag ends in a stale closure otherwise).
  const latest = useRef(layout);
  latest.current = layout;
  const [showDetail, setShowDetail] = useState(true);
  const scrollRequest = useRepo((s) => s.repos[tab]?.scrollRequest);
  const hasDiff = useRepo((s) => !!s.repos[tab]?.diff);
  const actions = useRepoActions(tab);
  const toast = useApp((s) => s.toast);

  // Saved widths are applied once, when they first arrive.
  const restored = useRef(false);
  useEffect(() => {
    if (restored.current || !saved?.leftWidth) return;
    restored.current = true;
    setLayout({ leftWidth: clamp(saved.leftWidth, LEFT), rightWidth: clamp(saved.rightWidth || RIGHT.def, RIGHT) });
  }, [saved]);
  useEffect(() => {
    useRepo.getState().refresh(tab);
  }, [tab]);
  // jump() asks for a scroll; the graph exposes it through its imperative handle.
  useEffect(() => {
    if (scrollRequest) graph.current?.scrollToRow(scrollRequest.row);
  }, [scrollRequest]);

  const run = (p: Promise<unknown>) => useApp.getState().trackWrite(tab, p).catch((e) => toast(e, tab));
  const clickCommit = () => root.current?.querySelector<HTMLButtonElement>('[data-command="commit:commit"]')?.click();

  // The same handlers back the toolbar buttons; only the active tab listens.
  useKeybindings(active ? {
    'repo:fetch': () => { void actions.fetch(); },
    'repo:stageAll': () => { void run(api.StageAll(tab)); },
    'repo:unstageAll': () => { void run(api.UnstageAll(tab)); },
    'repo:createBranch': () => { void actions.branch(); },
    'commit:commit': clickCommit,
    'commit:commitAll': async () => {
      useRepo.getState().select(tab, { kind: 'wip' });
      // Never commit on top of a failed stage: abort and let the toast explain.
      try {
        await useApp.getState().trackWrite(tab, api.StageAll(tab));
      } catch (e) {
        toast(e, tab);
        return;
      }
      // The commit button enables itself from the refreshed status: wait for it.
      await useRepo.getState().refreshStatus(tab);
      await new Promise((r) => setTimeout(r, 0));
      clickCommit();
    },
    'view:toggleDetailPanel': () => setShowDetail((x) => !x),
    'view:close': () => {
      const t = useApp.getState().tabs.find((x) => x.id === tab);
      if (t) closedTabs.push(t.path);
      void useApp.getState().close(tab);
    },
    'view:reopenTab': () => {
      const p = closedTabs.pop();
      if (p) void useApp.getState().open(p);
    },
    'workDir:focusCommitMessage': () => {
      useRepo.getState().select(tab, { kind: 'wip' });
      setTimeout(() => window.dispatchEvent(new Event('gitrais:focus-commit-message')), 0);
    },
  } : {});

  const persist = () => { void run(api.SaveLayout(latest.current as store.Layout)); };
  // The ref is updated synchronously here, so the onDone() that follows a key
  // press persists the new width rather than the one from the last render.
  const update = (next: typeof layout) => { latest.current = next; setLayout(next); };
  const resizeLeft = (dx: number) => update({ ...latest.current, leftWidth: clamp(latest.current.leftWidth + dx, LEFT) });
  const resizeRight = (dx: number) => update({ ...latest.current, rightWidth: clamp(latest.current.rightWidth + dx, RIGHT) });
  return (
    <div className="repo" ref={root}>
      <Toolbar tab={tab} />
      <div className="repo-body">
        <aside className="left-slot" style={{ width: layout.leftWidth }}><LeftPanel tab={tab} /></aside>
        <Resizer label="Larghezza pannello sinistro" value={layout.leftWidth} min={LEFT.min} max={LEFT.max}
          onResize={resizeLeft} onDone={persist} />
        <main className="center-slot">
          {hasDiff
            ? <Suspense fallback={<div className="muted diff-msg">Caricamento editor…</div>}><DiffView tab={tab} /></Suspense>
            : <GraphView tab={tab} handleRef={graph} />}
        </main>
        {showDetail && <>
          <Resizer invert label="Larghezza pannello dettagli" value={layout.rightWidth} min={RIGHT.min} max={RIGHT.max}
            onResize={resizeRight} onDone={persist} />
          <aside className="right-slot" style={{ width: layout.rightWidth }}><DetailPanel tab={tab} /></aside>
        </>}
      </div>
    </div>
  );
}
