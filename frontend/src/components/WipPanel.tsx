import { useEffect, useRef, useState } from 'react';
import { api, parseError } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { buildMessage, splitMessage } from '../detail/message';
import { confirmAction } from './Dialogs';
import { openMenu } from './ContextMenu';
import type { status, store } from '../../wailsjs/go/models';

const MAX_LISTED = 10;
// Same grouping the panel renders; also used to re-read the lists after an async confirm.
const groupFiles = (files: status.FileStatus[]) => ({
  conflicted: files.filter((f) => f.conflicted),
  unstaged: files.filter((f) => !f.conflicted && (f.workTree !== '.' || f.untracked)),
  staged: files.filter((f) => !f.conflicted && !f.untracked && f.index !== '.'),
});
const nameFiles = (paths: string[]) =>
  paths.slice(0, MAX_LISTED).join('\n') + (paths.length > MAX_LISTED ? `\n… e altri ${paths.length - MAX_LISTED}` : '');

export function WipPanel({ tab, path }: { tab: string; path: string }) {
  const st = useRepo((s) => s.repos[tab]?.status);
  const diff = useRepo((s) => s.repos[tab]?.diff);
  const openDiff = useRepo((s) => s.openDiff);
  const toast = useApp((s) => s.toast);
  // The persisted draft is read once at mount: later typing must never be overwritten by it.
  const [summary, setSummary] = useState(() => useApp.getState().state?.drafts?.[path]?.summary ?? '');
  const [body, setBody] = useState(() => useApp.getState().state?.drafts?.[path]?.body ?? '');
  const [amend, setAmend] = useState(false);
  const [busy, setBusy] = useState(false);
  const [hookErr, setHookErr] = useState('');
  const latest = useRef({ summary, body });
  const dirty = useRef(false);
  const saveTimer = useRef<number | undefined>(undefined);
  const busyRef = useRef(false);
  const amendSeq = useRef(0);
  // Message put into the form by the amend prefill, while it is still unedited.
  const prefill = useRef<{ summary: string; body: string } | null>(null);
  const summaryRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const focus = () => summaryRef.current?.focus();
    window.addEventListener('gitrais:focus-commit-message', focus);
    return () => window.removeEventListener('gitrais:focus-commit-message', focus);
  }, []);

  // Writes the draft to the backend and mirrors it in the app state, so that a remount
  // (e.g. selecting a commit and coming back) restores the latest text.
  const save = (d: { summary: string; body: string }) => {
    if (!path) return Promise.resolve();
    useApp.setState((s) => {
      if (!s.state) return s;
      const drafts = { ...s.state.drafts };
      if (d.summary || d.body) drafts[path] = d as store.Draft;
      else delete drafts[path];
      return { state: { ...s.state, drafts } as typeof s.state };
    });
    return api.SaveDraft(path, d as store.Draft).catch((e) => toast(e, tab));
  };
  const flush = () => {
    window.clearTimeout(saveTimer.current);
    if (!dirty.current) return;
    dirty.current = false;
    void save(latest.current);
  };
  const flushRef = useRef(flush);
  flushRef.current = flush;
  // Do not lose the last keystrokes when the panel goes away (selection change, tab close).
  useEffect(() => () => flushRef.current(), []);

  const persist = (s: string, b: string) => {
    latest.current = { summary: s, body: b };
    dirty.current = true;
    window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(flush, 500);
  };
  // Runs a mutation, then refreshes the status explicitly (watcher events may lag or be missed).
  const run = (p: Promise<unknown>) =>
    p.then(() => useRepo.getState().refreshStatus(tab)).catch((e) => toast(e, tab));
  const currentGroups = () => groupFiles(useRepo.getState().repos[tab]?.status?.files ?? []);

  const files = st?.files ?? [];
  const { conflicted, unstaged, staged } = groupFiles(files);

  const toggleAmend = async (on: boolean) => {
    const seq = ++amendSeq.current;
    setAmend(on);
    setHookErr('');
    const pf = prefill.current;
    if (!on && pf) {
      prefill.current = null;
      // Unchecking with the text still exactly the prefill restores the (empty) previous draft.
      if (latest.current.summary === pf.summary && latest.current.body === pf.body) {
        window.clearTimeout(saveTimer.current);
        dirty.current = false;
        latest.current = { summary: '', body: '' };
        setSummary(''); setBody('');
      }
    }
    if (on && !latest.current.summary && !latest.current.body) {
      try {
        const m = splitMessage(await api.LastCommitMessage(tab));
        // Ignore the answer if amend was toggled again, or the user typed, meanwhile.
        if (amendSeq.current !== seq || latest.current.summary || latest.current.body) return;
        // Save any pending (empty) edit first so the prefill is never flushed as a draft.
        flush();
        prefill.current = m;
        // The prefill is deliberately not persisted: it only becomes a draft once the user edits it.
        latest.current = m;
        setSummary(m.summary); setBody(m.body);
      } catch (e) {
        if (amendSeq.current === seq) setAmend(false);
        toast(e, tab);
      }
    }
  };
  const commit = async () => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setHookErr('');
    const sent = { ...latest.current };
    try {
      await api.Commit(tab, { message: buildMessage(sent.summary, sent.body), amend } as any);
      setAmend(false);
      prefill.current = null;
      void useRepo.getState().refreshStatus(tab);
      // Clear the form only if the user did not keep typing while the commit ran.
      if (latest.current.summary === sent.summary && latest.current.body === sent.body) {
        window.clearTimeout(saveTimer.current);
        dirty.current = false;
        latest.current = { summary: '', body: '' };
        setSummary(''); setBody('');
        await save(latest.current);
      }
    } catch (e) {
      const err = parseError(e);
      // A rejecting hook is shown next to the form, with its own output.
      if (err.kind === 'HookRejected') setHookErr(err.stderr.trim() || err.message || 'Hook rifiutato');
      else toast(e, tab);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };
  const discard = async (f: status.FileStatus) => {
    const message = f.untracked
      ? `Il file non tracciato ${f.path} verrà eliminato. Operazione irreversibile.`
      : `Scartare le modifiche a ${f.path}? Operazione irreversibile.`;
    if (!(await confirmAction({ title: f.untracked ? 'Elimina file' : 'Scarta modifiche', message, danger: true }))) return;
    // The status may have changed while the dialog was open: act only if the file is still listed as such.
    const cur = currentGroups().unstaged.find((g) => g.path === f.path && g.untracked === f.untracked);
    if (!cur) return;
    await run(cur.untracked ? api.Discard(tab, [], [cur.path]) : api.Discard(tab, [cur.path], []));
  };
  const discardAll = async () => {
    const tracked = unstaged.filter((f) => !f.untracked).map((f) => f.path);
    const untracked = unstaged.filter((f) => f.untracked).map((f) => f.path);
    const ok = await confirmAction({
      title: 'Scarta tutte le modifiche',
      message: `Scartare le modifiche a ${tracked.length + untracked.length} file? I file non tracciati verranno eliminati. Operazione irreversibile.\n\n${nameFiles([...tracked, ...untracked])}`,
      danger: true,
    });
    if (!ok) return;
    // Intersect with the lists as they are now: only files that were shown in the dialog are touched.
    const now = currentGroups().unstaged;
    const listed = new Set([...tracked, ...untracked]);
    const t = now.filter((f) => !f.untracked && listed.has(f.path)).map((f) => f.path);
    const u = now.filter((f) => f.untracked && listed.has(f.path)).map((f) => f.path);
    if (t.length || u.length) await run(api.Discard(tab, t, u));
  };

  const row = (f: status.FileStatus, group: 'conflict' | 'unstaged' | 'staged') => {
    const stagedList = group === 'staged';
    const active = diff?.kind === 'wip' && diff.path === f.path && diff.staged === stagedList;
    const letter = group === 'conflict' ? f.index + f.workTree : f.untracked ? 'A' : stagedList ? f.index : f.workTree;
    const cls = group === 'conflict' ? 'U' : letter;
    return (
      <li key={`${group}:${f.path}`} className={active ? 'selected' : ''}
        onContextMenu={group === 'unstaged' ? (e) => openMenu(e, [{ label: 'Scarta modifiche…', danger: true, onClick: () => discard(f) }]) : undefined}>
        <button className="file-row" aria-label={`Apri diff ${f.path}`} aria-current={active || undefined}
          onClick={() => openDiff(tab, { kind: 'wip', path: f.path, staged: stagedList, untracked: f.untracked })}>
          <span className={`st st-${cls}`}>{letter}</span>
          <span className="file-path"><bdi>{f.origPath ? `${f.origPath} → ${f.path}` : f.path}</bdi></span>
        </button>
        {group === 'unstaged' && <button className="file-btn" aria-label={`Scarta ${f.path}`} onClick={() => discard(f)}>Scarta</button>}
        {group === 'staged'
          ? <button className="file-btn" aria-label={`Unstage ${f.path}`} onClick={() => run(api.Unstage(tab, f.origPath ? [f.origPath, f.path] : [f.path]))}>Unstage</button>
          : <button className="file-btn" aria-label={group === 'conflict' ? `Segna risolto ${f.path}` : `Stage ${f.path}`}
              title={group === 'conflict' ? 'Aggiunge il file all’indice, segnando il conflitto come risolto' : undefined}
              onClick={() => run(api.Stage(tab, [f.path]))}>{group === 'conflict' ? 'Segna risolto' : 'Stage'}</button>}
      </li>
    );
  };
  const section = (id: string, title: string, items: status.FileStatus[], group: 'conflict' | 'unstaged' | 'staged', actions?: React.ReactNode) => (
    <section className="wip-section" aria-labelledby={`wip-${id}`}>
      <div className="wip-head">
        <h4 id={`wip-${id}`}>{title} <span className="muted">{items.length}</span></h4>
        {items.length > 0 && <span className="wip-actions">{actions}</span>}
      </div>
      <ul className="file-list">{items.map((f) => row(f, group))}</ul>
    </section>
  );

  return (
    <div className="detail wip">
      {conflicted.length > 0 && section('conflict', 'Conflitti', conflicted, 'conflict')}
      {section('unstaged', 'Non in stage', unstaged, 'unstaged', <>
        <button onClick={discardAll}>Scarta tutto</button>
        <button onClick={() => run(api.StageAll(tab))}>Stage tutto</button>
      </>)}
      {section('staged', 'In stage', staged, 'staged', <button onClick={() => run(api.UnstageAll(tab))}>Unstage tutto</button>)}
      <div className="commit-form">
        <input ref={summaryRef} aria-label="Oggetto del commit" placeholder="Oggetto del commit" value={summary} maxLength={200}
          onChange={(e) => { setSummary(e.target.value); persist(e.target.value, latest.current.body); }} />
        <span className={`muted counter ${summary.length > 72 ? 'warn' : ''}`} aria-hidden="true">{summary.length}/72</span>
        <textarea aria-label="Descrizione" placeholder="Descrizione" rows={5} value={body}
          onChange={(e) => { setBody(e.target.value); persist(latest.current.summary, e.target.value); }} />
        <label className="amend">
          <input type="checkbox" checked={amend} disabled={!!st?.unborn} onChange={(e) => toggleAmend(e.target.checked)} /> Amend
        </label>
        {hookErr && (
          <div className="hook-error" role="alert">
            <strong>Hook rifiutato</strong>
            <pre>{hookErr}</pre>
          </div>
        )}
        <button className="primary" data-command="commit:commit" disabled={busy || !summary.trim() || (!amend && staged.length === 0)} onClick={commit}>
          {amend ? 'Amend commit' : `Commit ${staged.length} file`}
        </button>
      </div>
    </div>
  );
}
