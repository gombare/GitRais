import { useApp } from '../store/app';

export function TabBar() {
  const { tabs, active, activate, close } = useApp();
  return (
    <div className="tabbar">
      {tabs.map((t) => (
        <div key={t.id} className={`tab ${t.id === active ? 'active' : ''}`} onClick={() => activate(t.id)} title={t.path}>
          <span>{t.name}</span>
          <button className="tab-close" onClick={(e) => { e.stopPropagation(); close(t.id); }} aria-label="Chiudi tab">×</button>
        </div>
      ))}
      <button className="tab-new" onClick={() => useApp.setState({ active: null })} aria-label="Nuova tab">+</button>
    </div>
  );
}
