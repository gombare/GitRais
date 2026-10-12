import { useEffect } from 'react';
import { useRepo } from '../store/repo';

// Shell only: the toolbar, sidebars and graph panels arrive in Tasks 23-27.
export function RepoView({ tab }: { tab: string }) {
  useEffect(() => {
    useRepo.getState().refresh(tab);
  }, [tab]);
  return (
    <div className="repo">
      <div className="toolbar-slot" />
      <div className="repo-body">
        <aside className="left-slot" />
        <main className="center-slot" />
        <aside className="right-slot" />
      </div>
    </div>
  );
}
