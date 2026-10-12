import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import type { gitread } from '../../wailsjs/go/models';

export function CommitDetails({ tab, hash }: { tab: string; hash: string }) {
  const [d, setD] = useState<gitread.CommitDetail | null>(null);
  const toast = useApp((s) => s.toast);
  const openDiff = useRepo((s) => s.openDiff);
  const jump = useRepo((s) => s.jump);
  const diff = useRepo((s) => s.repos[tab]?.diff);
  useEffect(() => {
    // `stale` drops the response of a selection that is no longer current.
    let stale = false;
    setD(null);
    api.CommitDetail(tab, hash).then((v) => { if (!stale) setD(v); }).catch((e) => { if (!stale) toast(e, tab); });
    return () => { stale = true; };
  }, [tab, hash, toast]);
  if (!d) return <div className="detail muted" aria-busy="true">Caricamento…</div>;
  const files = d.files ?? [];
  return (
    <div className="detail">
      <h3 className="detail-summary">{d.summary}</h3>
      {d.body && <pre className="detail-body">{d.body}</pre>}
      <dl className="detail-meta">
        <dt>Autore</dt><dd>{d.author} &lt;{d.authorEmail}&gt;</dd>
        <dt>Data</dt><dd>{new Date(d.authorTime * 1000).toLocaleString()}</dd>
        <dt>Commit</dt>
        <dd>
          <button className="link mono" title="Copia l’hash completo" aria-label={`Copia hash ${d.hash}`}
            onClick={() => navigator.clipboard?.writeText(d.hash).catch(() => undefined)}>{d.hash.slice(0, 10)}</button>
        </dd>
        <dt>Parent</dt>
        <dd>{(d.parents ?? []).map((p) => (
          <button key={p} className="link mono" onClick={() => jump(tab, p)}>{p.slice(0, 7)}</button>))}</dd>
      </dl>
      <h4>{files.length} file</h4>
      <ul className="file-list">
        {files.map((f) => {
          const active = diff?.kind === 'commit' && diff.hash === d.hash && diff.path === f.path;
          return (
            <li key={f.path} className={active ? 'selected' : ''}>
              <button className="file-row" aria-current={active || undefined}
                onClick={() => openDiff(tab, { kind: 'commit', hash: d.hash, path: f.path, oldPath: f.oldPath })}>
                <span className={`st st-${f.status}`}>{f.status}</span>
                <span className="file-path"><bdi>{f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}</bdi></span>
                <span className="muted">{f.binary ? 'bin' : `+${f.additions} −${f.deletions}`}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
