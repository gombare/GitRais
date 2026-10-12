import { useRef } from 'react';
import { useApp } from '../store/app';

export function TabBar() {
  const tabs = useApp((s) => s.tabs);
  const active = useApp((s) => s.active);
  const activate = useApp((s) => s.activate);
  const close = useApp((s) => s.close);
  const refs = useRef<Record<string, HTMLButtonElement | null>>({});

  const onKeyDown = (e: React.KeyboardEvent, index: number) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    const delta = e.key === 'ArrowRight' ? 1 : -1;
    const next = tabs[(index + delta + tabs.length) % tabs.length];
    activate(next.id);
    refs.current[next.id]?.focus();
  };

  return (
    <div className="tabbar" role="tablist" aria-label="Repository aperti">
      {tabs.map((t, i) => (
        <div key={t.id} className={`tab ${t.id === active ? 'active' : ''}`} role="presentation">
          <button
            ref={(el) => { refs.current[t.id] = el; }}
            role="tab"
            className="tab-label"
            aria-selected={t.id === active}
            tabIndex={t.id === active || (active === null && i === 0) ? 0 : -1}
            title={t.path}
            onClick={() => activate(t.id)}
            onKeyDown={(e) => onKeyDown(e, i)}
          >
            {t.name}
          </button>
          <button className="tab-close" onClick={() => close(t.id)} aria-label={`Chiudi ${t.name}`}>×</button>
        </div>
      ))}
      <button className="tab-new" onClick={() => useApp.setState({ active: null })} aria-label="Nuova tab">+</button>
    </div>
  );
}
