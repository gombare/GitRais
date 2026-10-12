import { useState } from 'react';
import { api } from '../api/client';
import { displayName } from '../api/paths';
import { useApp } from '../store/app';

export function Welcome() {
  const state = useApp((s) => s.state);
  const startupError = useApp((s) => s.startupError);
  const open = useApp((s) => s.open);
  const addTab = useApp((s) => s.addTab);
  const toast = useApp((s) => s.toast);
  const reloadState = useApp((s) => s.reloadState);
  const runOp = useApp((s) => s.runOp);
  const [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);
  const favorites = state?.favorites ?? [];

  const pickAndOpen = async () => {
    try {
      const dir = await api.PickDirectory('Apri repository');
      if (dir) await open(dir);
    } catch (e) {
      toast(e);
    }
  };
  const init = async () => {
    try {
      const dir = await api.PickDirectory('Cartella del nuovo repository');
      if (!dir) return;
      addTab(await api.InitRepo(dir));
      await reloadState();
    } catch (e) {
      toast(e);
    }
  };
  const clone = async () => {
    if (!url.trim()) return;
    let parent: string;
    try {
      parent = await api.PickDirectory('Dove clonare');
    } catch (e) {
      toast(e);
      return;
    }
    if (!parent) return;
    setBusy(true);
    try {
      // The backend derives the repository directory name from the URL.
      addTab(await runOp('', 'clone', () => api.CloneRepo(url.trim(), parent)));
      setUrl('');
      await reloadState();
    } catch (e) {
      toast(e); // Cancelled (via CancelClone) is silent
    } finally {
      setBusy(false);
    }
  };
  const star = async (p: string) => {
    try {
      await api.ToggleFavorite(p);
      await reloadState();
    } catch (e) {
      toast(e);
    }
  };

  return (
    <div className="welcome">
      <h1>Gitrais</h1>
      {startupError && (
        <div className="banner" role="status">
          <strong>Avvio con avvisi</strong>
          <pre>{startupError}</pre>
        </div>
      )}
      <div className="welcome-actions">
        <button onClick={pickAndOpen}>Apri repository…</button>
        <button onClick={init}>Nuovo repository…</button>
      </div>
      <div className="welcome-clone">
        <input placeholder="URL da clonare" value={url} onChange={(e) => setUrl(e.target.value)} />
        <button disabled={!url.trim() || busy} onClick={clone}>{busy ? 'Clonazione…' : 'Clona…'}</button>
        {busy && <button onClick={() => api.CancelClone().catch((e) => toast(e))}>Annulla</button>}
      </div>
      {([['Preferiti', favorites], ['Recenti', state?.recents ?? []]] as const).map(([title, list]) => (
        <section key={title}>
          <h2>{title}</h2>
          <ul className="repo-list">
            {list.map((p) => (
              <li key={p}>
                <button className="link" onClick={() => open(p)}>{displayName(p)}</button>
                <span className="muted">{p}</span>
                <button className="icon" onClick={() => star(p)} aria-label="Preferito" aria-pressed={favorites.includes(p)}>
                  {favorites.includes(p) ? '★' : '☆'}
                </button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
