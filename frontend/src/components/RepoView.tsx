import { useEffect, useRef, useState } from 'react';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { GraphView, type GraphHandle } from './GraphView';
import { LeftPanel } from './LeftPanel';
import { DetailPanel } from './DetailPanel';

// Shell: the toolbar and right panel arrive in Tasks 25-27.
export function RepoView({ tab }: { tab: string }) {
  const graph = useRef<GraphHandle>(null);
  const leftWidth = useApp((s) => s.state?.layout?.leftWidth) || 260;
  const rightWidth = useApp((s) => s.state?.layout?.rightWidth) || 340;
  // The toggle (view:toggleDetailPanel) is wired in Task 27.
  const [showDetail] = useState(true);
  const scrollRequest = useRepo((s) => s.repos[tab]?.scrollRequest);
  useEffect(() => {
    useRepo.getState().refresh(tab);
  }, [tab]);
  // jump() asks for a scroll; the graph exposes it through its imperative handle.
  useEffect(() => {
    if (scrollRequest) graph.current?.scrollToRow(scrollRequest.row);
  }, [scrollRequest]);
  return (
    <div className="repo">
      <div className="toolbar-slot" />
      <div className="repo-body">
        <aside className="left-slot" style={{ width: leftWidth }}><LeftPanel tab={tab} /></aside>
        <main className="center-slot"><GraphView tab={tab} handleRef={graph} /></main>
        {showDetail && <aside className="right-slot" style={{ width: rightWidth }}><DetailPanel tab={tab} /></aside>}
      </div>
    </div>
  );
}
