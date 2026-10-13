import { useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react';
import { DiffEditor } from '@monaco-editor/react';
import type { editor } from 'monaco-editor/editor/editor.api';
import { api, on, parseError } from '../api/client';
import { useApp } from '../store/app';
import { useRepo, type DiffTarget } from '../store/repo';
import { languageFor } from '../monaco';
import { fingerprint, modelMatchesDiff, selectionMatchesModel, selectionToLines, splitLikeMonaco } from '../diff/selection';
import type { diff, ops, session } from '../../wailsjs/go/models';

// The two sides of the file and the parsed diff, always from the same load:
// hunk and line indexes sent to StageLines refer to this `fd`.
type Loaded = { v: ops.Versions; fd: diff.FileDiff };

const uiError = (kind: string, message = '') => JSON.stringify({ kind, message, stderr: '' });
const viewMismatch = () => uiError('Unknown', 'La vista non corrisponde al diff: aggiornare');

type DiffEditorProps = Parameters<typeof DiffEditor>[0];

// The Monaco diff editor, publishing its instance in edRef while mounted: the
// ref is cleared on unmount so no disposed editor is ever read.
function MonacoDiff({ edRef, ...props }: DiffEditorProps & { edRef: MutableRefObject<editor.IStandaloneDiffEditor | null> }) {
  useEffect(() => () => { edRef.current = null; }, [edRef]);
  return <DiffEditor {...props} onMount={(e) => { edRef.current = e; }} />;
}

function selectedLines(ed: editor.ICodeEditor): Set<number> {
  const out = new Set<number>();
  for (const s of ed.getSelections() ?? []) {
    if (s.isEmpty()) continue;
    // A selection ending at column 1 does not include that line.
    const end = s.endLineNumber > s.startLineNumber && s.endColumn === 1 ? s.endLineNumber - 1 : s.endLineNumber;
    for (let l = s.startLineNumber; l <= end; l++) out.add(l);
  }
  return out;
}

function versionsRequest(t: DiffTarget): ops.VersionsRequest {
  return t.kind === 'commit'
    ? { path: t.path, oldPath: t.oldPath, staged: false, untracked: false, commit: t.hash }
    : { path: t.path, oldPath: t.oldPath ?? '', staged: t.staged, untracked: t.untracked, commit: '' };
}

// Side-by-side (or unified) Monaco diff of repos[tab].diff, with stage/unstage of
// the whole file, of single hunks and of the selected lines (WIP, modified text files).
export function DiffView({ tab }: { tab: string }) {
  const target = useRepo((s) => s.repos[tab]?.diff ?? null);
  const status = useRepo((s) => s.repos[tab]?.status);
  const openDiff = useRepo((s) => s.openDiff);
  const toast = useApp((s) => s.toast);
  const [data, setData] = useState<Loaded | null>(null);
  // True while the latest load is in flight: the shown hunks may be stale, so partial staging is off.
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  // True after a failed reload: the shown diff may be outdated until a load succeeds.
  const [stale, setStale] = useState(false);
  const [inline, setInline] = useState(false);
  const ed = useRef<editor.IStandaloneDiffEditor | null>(null);
  const seq = useRef(0);
  const targetRef = useRef(target);
  targetRef.current = target;

  // Loads both sides and the diff for the current target; out-of-order responses are dropped.
  const load = useCallback(async () => {
    const t = targetRef.current;
    const n = ++seq.current;
    if (!t) return;
    setLoading(true);
    try {
      const [v, fd] = await Promise.all([
        api.FileVersions(tab, versionsRequest(t)),
        t.kind === 'commit'
          ? api.CommitFileDiff(tab, t.hash, t.path, t.oldPath)
          : api.WipDiff(tab, { path: t.path, staged: t.staged, untracked: t.untracked, oldPath: t.oldPath ?? '' } as ops.WipDiffRequest),
      ]);
      if (n === seq.current) { setData({ v, fd }); setStale(false); }
    } catch (e) {
      if (n === seq.current) { setStale(true); toast(e, tab); }
    } finally {
      if (n === seq.current) setLoading(false);
    }
  }, [tab, toast]);

  // A new target: drop what is shown and load it.
  useEffect(() => {
    setData(null);
    void load();
  }, [target, load]);

  // WIP diffs follow the working tree: reload on every wip:changed (marking the diff stale
  // right away) and on every status refresh.
  const wip = target?.kind === 'wip';
  useEffect(() => {
    if (!wip) return;
    return on<{ tab: string }>('wip:changed', (e) => {
      if (e.tab !== tab) return;
      setLoading(true);
      void load();
    });
  }, [wip, tab, load]);
  const first = useRef(true);
  useEffect(() => {
    if (first.current) { first.current = false; return; }
    if (wip) void load();
  }, [status]); // eslint-disable-line react-hooks/exhaustive-deps

  // Whether Monaco's lines are git's lines (no lone "\r", no clean filter): only then
  // can a selection be mapped to hunk lines. Monaco splits its models the same way.
  const linesMapped = useMemo(
    () => !!data && modelMatchesDiff(data.fd, splitLikeMonaco(data.v.old), splitLikeMonaco(data.v.new)),
    [data],
  );

  if (!target) return null;
  const staged = target.kind === 'wip' && target.staged;
  const verb = staged ? 'Unstage' : 'Stage';
  const fd = data?.fd;
  const v = data?.v;
  const conflicted = wip && fd?.status === 'U';
  const binary = !!(v?.binary || fd?.binary);
  const tooLarge = !!v?.tooLarge;
  const noChanges = !!fd && !conflicted && !binary && !tooLarge && fd.hunks.length === 0 && v?.old === v?.new;
  const partial = target.kind === 'wip' && !target.untracked && fd?.status === 'M' && !binary && !tooLarge && fd.hunks.length > 0;
  const partialDisabled = loading || busy || stale;

  // Runs a mutation, then refreshes the status and this diff (stale responses are dropped by load).
  const mutate = async (fn: () => Promise<unknown>, after?: () => void) => {
    setBusy(true);
    try {
      await useApp.getState().trackWrite(tab, fn());
      void useRepo.getState().refreshStatus(tab);
      // A new target reloads through the target effect.
      if (after) after();
      else await load();
    } catch (e) {
      toast(e, tab);
      // The file changed since it was shown: show the current diff.
      if (parseError(e).kind === 'DiffChanged') void load();
    } finally {
      setBusy(false);
    }
  };
  // Indexes refer to the displayed fd; the fingerprint lets the backend refuse them if the file changed.
  const stageLines = (shown: diff.FileDiff, hunk: number, lines: number[] = []) =>
    mutate(() => api.StageLines(tab, {
      path: target.path, staged, hunk, lines, expect: fingerprint(shown, hunk, lines),
    } as session.StageLinesRequest));
  const stageSelection = () => {
    const e = ed.current;
    if (!fd || !e) return;
    const models = e.getModel();
    if (!models || !linesMapped) return toast(viewMismatch(), tab);
    const sel = selectionToLines(fd, selectedLines(e.getOriginalEditor()), selectedLines(e.getModifiedEditor()));
    if (!sel) return toast(uiError('NoLinesSelected'), tab);
    if ('error' in sel) return toast(uiError('Unknown', sel.error), tab);
    // Monaco and git may split lines differently: check the actual model lines too.
    if (!modelMatchesDiff(fd, models.original, models.modified) || !selectionMatchesModel(fd, sel, models.original, models.modified)) {
      return toast(viewMismatch(), tab);
    }
    void stageLines(fd, sel.hunk, sel.lines);
  };
  // Whole file: afterwards the diff follows the file to the other list.
  const stageFile = () => {
    if (target.kind !== 'wip') return;
    const paths = staged && target.oldPath ? [target.oldPath, target.path] : [target.path];
    void mutate(() => (staged ? api.Unstage(tab, paths) : api.Stage(tab, paths)), () =>
      openDiff(tab, staged
        ? { kind: 'wip', path: target.path, staged: false, untracked: fd?.status === 'A' }
        : { kind: 'wip', path: target.path, staged: true, untracked: false }));
  };

  return (
    <div className="diffview" aria-busy={loading || undefined}>
      <div className="diff-head">
        <strong className="file-path" title={target.path}>
          <bdi>{target.kind === 'commit' && target.oldPath && target.oldPath !== target.path ? `${target.oldPath} → ${target.path}` : target.path}</bdi>
        </strong>
        <span className="diff-head-actions">
          {target.kind === 'wip' && !conflicted && (
            <button onClick={stageFile} disabled={busy} aria-label={`${verb} file ${target.path}`}>{verb} file</button>
          )}
          {partial && (
            <button onClick={stageSelection} disabled={partialDisabled || !linesMapped}
              title={linesMapped
                ? 'Seleziona righe modificate in uno dei due lati del diff'
                : 'Le righe mostrate non corrispondono a quelle di git (fine riga o filtri): usa i blocchi o il file intero'}>
              {verb} righe selezionate
            </button>
          )}
          {!binary && !tooLarge && !conflicted && (
            <button onClick={() => setInline((x) => !x)} aria-pressed={inline}>{inline ? 'Affiancato' : 'Unificato'}</button>
          )}
          <button className="icon" onClick={() => openDiff(tab, null)} aria-label="Chiudi diff" title="Chiudi diff">×</button>
        </span>
      </div>
      {partial && fd && (
        <div className="hunk-bar" role="toolbar" aria-label="Blocchi di modifiche">
          {fd.hunks.map((h, i) => (
            <button key={i} disabled={partialDisabled} onClick={() => void stageLines(fd, i)}
              onFocus={() => ed.current?.getModifiedEditor().revealLineInCenterIfOutsideViewport(Math.max(1, h.newStart))}
              aria-label={`${verb} blocco ${i + 1} (righe ${h.newStart}–${h.newStart + Math.max(h.newLines, 1) - 1})`}>
              {verb} blocco @{h.newStart}
            </button>
          ))}
        </div>
      )}
      {conflicted && (
        <div className="muted diff-msg" role="status">
          File in conflitto: risolvi i conflitti nel file, poi usa «Segna risolto» nel pannello delle modifiche.
        </div>
      )}
      {!conflicted && binary && <div className="muted diff-msg" role="status">File binario: anteprima non disponibile.</div>}
      {!conflicted && tooLarge && <div className="muted diff-msg" role="status">File troppo grande per il diff (oltre 5 MB).</div>}
      {noChanges && <div className="muted diff-msg" role="status">Nessuna modifica da mostrare.</div>}
      {!data && loading && <div className="muted diff-msg">Caricamento…</div>}
      {v && fd && !conflicted && !binary && !tooLarge && !noChanges && (
        <div className="diff-editor">
          <MonacoDiff
            edRef={ed} original={v.old} modified={v.new} language={languageFor(target.path)} theme="vs-dark"
            options={{
              readOnly: true, originalEditable: false, renderSideBySide: !inline, useInlineViewWhenSpaceIsLimited: false,
              minimap: { enabled: false }, scrollBeyondLastLine: false, automaticLayout: true, renderOverviewRuler: false,
            }}
          />
        </div>
      )}
    </div>
  );
}

export default DiffView;
