// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

type Sel = { startLineNumber: number; endLineNumber: number; endColumn: number; isEmpty: () => boolean };
const editorState = vi.hoisted(() => ({ orig: [] as Sel[], mod: [] as Sel[] }));
const handlers = vi.hoisted(() => new Map<string, (d: unknown) => void>());

const api = vi.hoisted(() => ({
  FileVersions: vi.fn(async (..._a: unknown[]): Promise<any> => ({ old: 'a\n', new: 'b\n', binary: false, tooLarge: false })),
  WipDiff: vi.fn(async (..._a: unknown[]): Promise<any> => null),
  CommitFileDiff: vi.fn(async (..._a: unknown[]): Promise<any> => null),
  StageLines: vi.fn(async (..._a: unknown[]) => undefined),
  Stage: vi.fn(async (..._a: unknown[]) => undefined),
  Unstage: vi.fn(async (..._a: unknown[]) => undefined),
  Status: vi.fn(async (..._a: unknown[]): Promise<any> => ({ files: [] })),
}));
vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  api,
  on: (ev: string, cb: (d: unknown) => void) => { handlers.set(ev, cb); return () => handlers.delete(ev); },
}));
vi.mock('../monaco', () => ({ languageFor: () => 'plaintext' }));
vi.mock('@monaco-editor/react', () => ({
  DiffEditor: (p: { original: string; modified: string; onMount: (e: unknown) => void }) => {
    p.onMount({
      getOriginalEditor: () => ({ getSelections: () => editorState.orig }),
      getModifiedEditor: () => ({ getSelections: () => editorState.mod, revealLineInCenterIfOutsideViewport: () => {} }),
    });
    return <pre data-testid="editor">{`${p.original}|${p.modified}`}</pre>;
  },
}));

import { useApp } from '../store/app';
import { useRepo, type DiffTarget } from '../store/repo';
import { DiffView } from './DiffView';
import { Toasts } from './Toasts';

const sel = (a: number, b: number): Sel => ({ startLineNumber: a, endLineNumber: b, endColumn: 3, isEmpty: () => false });
const modified = () => ({
  oldPath: 'a.txt', newPath: 'a.txt', status: 'M', binary: false,
  hunks: [
    { newStart: 1, newLines: 2, lines: [{ kind: ' ', oldNo: 1, newNo: 1 }, { kind: '-', oldNo: 2, newNo: 0 }, { kind: '+', oldNo: 0, newNo: 2 }] },
    { newStart: 10, newLines: 1, lines: [{ kind: '+', oldNo: 0, newNo: 10 }] },
  ],
});
const setTarget = (diff: DiffTarget | null) =>
  useRepo.setState({ repos: { t: { stashes: [], graphRows: 0, graphDone: true, graphVersion: 0, selection: { kind: 'wip' }, diff } } });
const deferred = <T,>() => {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
};

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockClear());
  api.WipDiff.mockImplementation(async () => modified());
  api.FileVersions.mockImplementation(async () => ({ old: 'a\n', new: 'b\n', binary: false, tooLarge: false }));
  editorState.orig = []; editorState.mod = [];
  handlers.clear();
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }] as any, active: 't', toasts: [], ops: {} });
  setTarget({ kind: 'wip', path: 'a.txt', staged: false, untracked: false });
});
afterEach(cleanup);

const mount = () => render(<><DiffView tab="t" /><Toasts /></>);

describe('DiffView', () => {
  it('loads both sides and the diff, and stages a hunk', async () => {
    mount();
    expect((await screen.findByTestId('editor')).textContent).toBe('a\n|b\n');
    expect(api.FileVersions).toHaveBeenCalledWith('t', { path: 'a.txt', oldPath: '', staged: false, untracked: false, commit: '' });
    fireEvent.click(screen.getByRole('button', { name: /Stage blocco 2/ }));
    await waitFor(() => expect(api.StageLines).toHaveBeenCalledWith('t', { path: 'a.txt', staged: false, hunk: 1, lines: [] }));
    await waitFor(() => expect(api.WipDiff.mock.calls.length).toBeGreaterThanOrEqual(2));
    expect(api.Status).toHaveBeenCalled();
  });

  it('stages the selected lines of one hunk; unstages in the staged view', async () => {
    setTarget({ kind: 'wip', path: 'a.txt', staged: true, untracked: false });
    mount();
    await screen.findByTestId('editor');
    editorState.orig = [sel(2, 2)]; editorState.mod = [sel(2, 2)];
    fireEvent.click(screen.getByRole('button', { name: 'Unstage righe selezionate' }));
    await waitFor(() => expect(api.StageLines).toHaveBeenCalledWith('t', { path: 'a.txt', staged: true, hunk: 0, lines: [1, 2] }));
  });

  it('rejects a selection across hunks or without changed lines', async () => {
    mount();
    await screen.findByTestId('editor');
    editorState.mod = [sel(2, 10)];
    fireEvent.click(screen.getByRole('button', { name: 'Stage righe selezionate' }));
    expect(await screen.findByText(/un solo blocco/)).toBeTruthy();
    editorState.mod = [sel(1, 1)];
    fireEvent.click(screen.getByRole('button', { name: 'Stage righe selezionate' }));
    await waitFor(() => expect(screen.getAllByRole('alert').length + screen.queryAllByText(/Nessuna riga/).length).toBeGreaterThan(0));
    expect(api.StageLines).not.toHaveBeenCalled();
  });

  it('disables partial staging while a wip:changed refresh is pending', async () => {
    mount();
    await screen.findByTestId('editor');
    const d = deferred<any>();
    api.WipDiff.mockImplementationOnce(() => d.promise);
    act(() => handlers.get('wip:changed')!({ tab: 't' }));
    expect((screen.getByRole('button', { name: 'Stage righe selezionate' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: /Stage blocco 1/ }) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => d.resolve(modified()));
    expect((screen.getByRole('button', { name: 'Stage righe selezionate' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('drops stale responses when the target changes', async () => {
    const slow = deferred<any>();
    api.FileVersions.mockImplementationOnce(() => slow.promise);
    mount();
    api.FileVersions.mockImplementation(async () => ({ old: 'new-old', new: 'new-new', binary: false, tooLarge: false }));
    act(() => setTarget({ kind: 'wip', path: 'b.txt', staged: false, untracked: false }));
    expect((await screen.findByTestId('editor')).textContent).toBe('new-old|new-new');
    await act(async () => slow.resolve({ old: 'stale', new: 'stale', binary: false, tooLarge: false }));
    expect(screen.getByTestId('editor').textContent).toBe('new-old|new-new');
  });

  it('conflicted file: message, no editor, no staging', async () => {
    api.WipDiff.mockImplementation(async () => ({ oldPath: 'a.txt', newPath: 'a.txt', status: 'U', binary: false, hunks: [] }));
    mount();
    expect(await screen.findByText(/File in conflitto/)).toBeTruthy();
    expect(screen.queryByTestId('editor')).toBeNull();
    expect(screen.queryByRole('button', { name: /Stage/ })).toBeNull();
  });

  it('binary file: message and file-level staging only; the diff follows the file', async () => {
    api.FileVersions.mockImplementation(async () => ({ old: '', new: '', binary: true, tooLarge: false }));
    mount();
    expect(await screen.findByText(/File binario/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /righe selezionate|blocco/ })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Stage file a.txt' }));
    await waitFor(() => expect(api.Stage).toHaveBeenCalledWith('t', ['a.txt']));
    await waitFor(() => expect(useRepo.getState().repos.t.diff).toEqual({ kind: 'wip', path: 'a.txt', staged: true, untracked: false }));
  });

  it('too large: message only', async () => {
    api.FileVersions.mockImplementation(async () => ({ old: '', new: '', binary: false, tooLarge: true }));
    mount();
    expect(await screen.findByText(/troppo grande/)).toBeTruthy();
    expect(screen.queryByTestId('editor')).toBeNull();
  });

  it('staged rename: old side from oldPath, unstage both paths', async () => {
    api.WipDiff.mockImplementation(async () => ({ ...modified(), status: 'R', oldPath: 'old.txt' }));
    setTarget({ kind: 'wip', path: 'a.txt', staged: true, untracked: false, oldPath: 'old.txt' });
    mount();
    await screen.findByTestId('editor');
    expect(api.FileVersions).toHaveBeenCalledWith('t', { path: 'a.txt', oldPath: 'old.txt', staged: true, untracked: false, commit: '' });
    expect(screen.queryByRole('button', { name: /righe selezionate/ })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Unstage file a.txt' }));
    await waitFor(() => expect(api.Unstage).toHaveBeenCalledWith('t', ['old.txt', 'a.txt']));
  });

  it('commit diff: versions of the commit, no staging controls', async () => {
    api.CommitFileDiff.mockImplementation(async () => modified());
    setTarget({ kind: 'commit', hash: 'h'.repeat(40), path: 'a.txt', oldPath: '' });
    mount();
    await screen.findByTestId('editor');
    expect(api.FileVersions).toHaveBeenCalledWith('t', { path: 'a.txt', oldPath: '', staged: false, untracked: false, commit: 'h'.repeat(40) });
    expect(screen.queryByRole('button', { name: /Stage|Unstage/ })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Chiudi diff' }));
    expect(useRepo.getState().repos.t.diff).toBeNull();
  });
});
