import { useEffect } from 'react';
import { useApp } from './store/app';
import { useBackendEvents } from './hooks/useBackendEvents';
import { TabBar } from './components/TabBar';
import { Welcome } from './components/Welcome';
import { Toasts } from './components/Toasts';
import { RepoView } from './components/RepoView';
import { DialogHost } from './components/Dialogs';
import { AuthDialog } from './components/AuthDialog';
import { ContextMenuHost } from './components/ContextMenu';

export default function App() {
  const active = useApp((s) => s.active);
  const init = useApp((s) => s.init);
  const tabs = useApp((s) => s.tabs);
  useBackendEvents();
  useEffect(() => {
    init();
  }, [init]);
  return (
    <div className="app">
      <TabBar />
      {tabs.map((t) => (
        <div key={t.id} className="tab-content" hidden={t.id !== active}>
          <RepoView tab={t.id} />
        </div>
      ))}
      {active === null && <Welcome />}
      <Toasts />
      <DialogHost />
      <AuthDialog />
      <ContextMenuHost />
    </div>
  );
}
