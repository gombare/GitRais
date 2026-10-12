// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({
  Stage: vi.fn(async (..._a: unknown[]) => undefined), Unstage: vi.fn(async (..._a: unknown[]) => undefined),
  StageAll: vi.fn(async (..._a: unknown[]) => undefined), UnstageAll: vi.fn(async (..._a: unknown[]) => undefined),
  Discard: vi.fn(async (..._a: unknown[]) => undefined), Commit: vi.fn(async (..._a: unknown[]) => 'abc'),
  SaveDraft: vi.fn(async (..._a: unknown[]) => undefined), LastCommitMessage: vi.fn(async (..._a: unknown[]) => 'old\n\nbody'),
}));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { WipPanel } from './WipPanel';
import { DialogHost } from './Dialogs';
import { Toasts } from './Toasts';

const f = (path: string, index: string, workTree: string, extra = {}) =>
  ({ path, origPath: '', index, workTree, conflicted: false, untracked: false, ...extra }) as any;

const setFiles = (files: any[], unborn = false) =>
  useRepo.setState({ repos: { t: { stashes: [], graphRows: 0, graphDone: true, graphVersion: 0, selection: { kind: 'wip' }, diff: null,
    status: { branch: 'main', detached: false, unborn, upstream: '', ahead: 0, behind: 0, files } as any } } });

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockClear());
  api.Commit.mockResolvedValue('abc');
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [], ops: {}, state: { drafts: {} } as any });
  setFiles([f('a.txt', '.', 'M'), f('b.txt', 'M', '.'), f('new.txt', '?', '?', { untracked: true }), f('c.txt', 'U', 'U', { conflicted: true })]);
});
afterEach(() => { cleanup(); vi.useRealTimers(); });

const mount = () => render(<><WipPanel tab="t" path="/r" /><DialogHost /><Toasts /></>);

describe('WipPanel', () => {
  it('groups files: conflicted, unstaged (incl. untracked), staged', () => {
    mount();
    expect(screen.getByRole('region', { name: /Conflitti/ }).textContent).toContain('c.txt');
    const un = screen.getByRole('region', { name: /Non in stage/ }).textContent!;
    expect(un).toContain('a.txt'); expect(un).toContain('new.txt'); expect(un).not.toContain('b.txt'); expect(un).not.toContain('c.txt');
    expect(screen.getByRole('region', { name: /^In stage/ }).textContent).toContain('b.txt');
  });
  it('stages and unstages single files and all', () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Stage a.txt' }));
    expect(api.Stage).toHaveBeenCalledWith('t', ['a.txt']);
    fireEvent.click(screen.getByRole('button', { name: 'Unstage b.txt' }));
    expect(api.Unstage).toHaveBeenCalledWith('t', ['b.txt']);
    fireEvent.click(screen.getByRole('button', { name: 'Stage tutto' }));
    expect(api.StageAll).toHaveBeenCalledWith('t');
    fireEvent.click(screen.getByRole('button', { name: 'Unstage tutto' }));
    expect(api.UnstageAll).toHaveBeenCalledWith('t');
  });
  it('conflicted file: staging marks it resolved', () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Segna risolto c.txt' }));
    expect(api.Stage).toHaveBeenCalledWith('t', ['c.txt']);
  });
  it('discard asks confirmation naming the file; untracked go through the untracked list', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Scarta a.txt' }));
    expect(api.Discard).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog').textContent).toContain('a.txt');
    fireEvent.click(screen.getByRole('button', { name: 'OK' }));
    await waitFor(() => expect(api.Discard).toHaveBeenCalledWith('t', ['a.txt'], []));
    fireEvent.click(screen.getByRole('button', { name: 'Scarta new.txt' }));
    fireEvent.click(screen.getByRole('button', { name: 'OK' }));
    await waitFor(() => expect(api.Discard).toHaveBeenCalledWith('t', [], ['new.txt']));
  });
  it('discard all names the files and excludes conflicted ones; cancel does nothing', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Scarta tutto' }));
    const txt = screen.getByRole('dialog').textContent!;
    expect(txt).toContain('a.txt'); expect(txt).toContain('new.txt'); expect(txt).not.toContain('c.txt');
    fireEvent.click(screen.getByRole('button', { name: 'Annulla' }));
    await act(async () => {});
    expect(api.Discard).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Scarta tutto' }));
    fireEvent.click(screen.getByRole('button', { name: 'OK' }));
    await waitFor(() => expect(api.Discard).toHaveBeenCalledWith('t', ['a.txt'], ['new.txt']));
  });
  it('opens the diff for a file', () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Apri diff b.txt' }));
    expect(useRepo.getState().repos.t.diff).toEqual({ kind: 'wip', path: 'b.txt', staged: true, untracked: false });
  });
  it('commit sends the built message, clears the form and the draft', async () => {
    mount();
    fireEvent.change(screen.getByLabelText('Oggetto del commit'), { target: { value: ' fix ' } });
    fireEvent.change(screen.getByLabelText('Descrizione'), { target: { value: 'why\n' } });
    fireEvent.click(screen.getByRole('button', { name: /^Commit 1 file/ }));
    await waitFor(() => expect(api.Commit).toHaveBeenCalledWith('t', { message: 'fix\n\nwhy', amend: false }));
    await waitFor(() => expect((screen.getByLabelText('Oggetto del commit') as HTMLInputElement).value).toBe(''));
    expect(api.SaveDraft).toHaveBeenLastCalledWith('/r', { summary: '', body: '' });
  });
  it('commit is disabled without summary or staged files', () => {
    mount();
    const btn = screen.getByRole('button', { name: /^Commit/ }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('Oggetto del commit'), { target: { value: 'x' } });
    expect(btn.disabled).toBe(false);
  });
  it('shows hook stderr when the commit is rejected, and keeps the message', async () => {
    api.Commit.mockRejectedValueOnce(JSON.stringify({ kind: 'HookRejected', message: 'hook', stderr: 'lint failed: no TODO' }));
    mount();
    fireEvent.change(screen.getByLabelText('Oggetto del commit'), { target: { value: 'x' } });
    fireEvent.click(screen.getByRole('button', { name: /^Commit/ }));
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('lint failed: no TODO'));
    expect((screen.getByLabelText('Oggetto del commit') as HTMLInputElement).value).toBe('x');
  });
  it('other commit errors go to a toast', async () => {
    api.Commit.mockRejectedValueOnce(JSON.stringify({ kind: 'NothingToCommit', message: '', stderr: '' }));
    mount();
    fireEvent.change(screen.getByLabelText('Oggetto del commit'), { target: { value: 'x' } });
    fireEvent.click(screen.getByRole('button', { name: /^Commit/ }));
    await waitFor(() => expect(useApp.getState().toasts[0]?.error.kind).toBe('NothingToCommit'));
  });
  it('amend prefills from the last commit message, only when the form is empty', async () => {
    mount();
    fireEvent.click(screen.getByLabelText('Amend'));
    await waitFor(() => expect((screen.getByLabelText('Oggetto del commit') as HTMLInputElement).value).toBe('old'));
    expect((screen.getByLabelText('Descrizione') as HTMLTextAreaElement).value).toBe('body');
    fireEvent.click(screen.getByRole('button', { name: 'Amend commit' }));
    await waitFor(() => expect(api.Commit).toHaveBeenCalledWith('t', { message: 'old\n\nbody', amend: true }));
  });
  it('unstaging a staged rename passes both paths and the row shows old -> new', () => {
    setFiles([f('new.txt', 'R', '.', { origPath: 'old.txt' })]);
    mount();
    expect(screen.getByRole('region', { name: /^In stage/ }).textContent).toContain('old.txt → new.txt');
    fireEvent.click(screen.getByRole('button', { name: 'Unstage new.txt' }));
    expect(api.Unstage).toHaveBeenCalledWith('t', ['old.txt', 'new.txt']);
  });
  it('amend prefill is never persisted as a draft and is dropped when amend is unchecked', async () => {
    const { unmount } = mount();
    fireEvent.click(screen.getByLabelText('Amend'));
    await waitFor(() => expect((screen.getByLabelText('Oggetto del commit') as HTMLInputElement).value).toBe('old'));
    fireEvent.click(screen.getByLabelText('Amend'));
    expect((screen.getByLabelText('Oggetto del commit') as HTMLInputElement).value).toBe('');
    expect((screen.getByLabelText('Descrizione') as HTMLTextAreaElement).value).toBe('');
    unmount();
    expect(api.SaveDraft.mock.calls.some((c) => (c[1] as any)?.summary === 'old')).toBe(false);
    expect((useApp.getState().state as any).drafts['/r']).toBeUndefined();
  });
  it('single discard of an untracked file says it will be deleted', () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Scarta new.txt' }));
    expect(screen.getByRole('dialog').textContent).toContain('eliminato');
  });
  it('discard all only acts on files still listed after the confirm', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Scarta tutto' }));
    act(() => setFiles([f('new.txt', '?', '?', { untracked: true })]));
    fireEvent.click(screen.getByRole('button', { name: 'OK' }));
    await waitFor(() => expect(api.Discard).toHaveBeenCalledWith('t', [], ['new.txt']));
  });
  it('refreshes the status after a successful stage', async () => {
    const spy = vi.fn(async () => undefined);
    useRepo.setState({ refreshStatus: spy });
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Stage a.txt' }));
    await waitFor(() => expect(spy).toHaveBeenCalledWith('t'));
  });
  it('amend is unavailable on an unborn branch', () => {
    setFiles([f('a.txt', 'A', '.')], true);
    mount();
    expect((screen.getByLabelText('Amend') as HTMLInputElement).disabled).toBe(true);
  });
  it('saves the draft (debounced) and restores it on mount', async () => {
    vi.useFakeTimers();
    const { unmount } = mount();
    fireEvent.change(screen.getByLabelText('Oggetto del commit'), { target: { value: 's1' } });
    fireEvent.change(screen.getByLabelText('Descrizione'), { target: { value: 'b1' } });
    expect(api.SaveDraft).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(600); });
    expect(api.SaveDraft).toHaveBeenCalledTimes(1);
    expect(api.SaveDraft).toHaveBeenCalledWith('/r', { summary: 's1', body: 'b1' });
    unmount();
    cleanup();
    mount();
    expect((screen.getByLabelText('Oggetto del commit') as HTMLInputElement).value).toBe('s1');
    expect((screen.getByLabelText('Descrizione') as HTMLTextAreaElement).value).toBe('b1');
  });
  it('flushes a pending draft on unmount', () => {
    vi.useFakeTimers();
    const { unmount } = mount();
    fireEvent.change(screen.getByLabelText('Oggetto del commit'), { target: { value: 'late' } });
    unmount();
    expect(api.SaveDraft).toHaveBeenCalledWith('/r', { summary: 'late', body: '' });
  });
  it('restores a persisted draft from app state', () => {
    useApp.setState({ state: { drafts: { '/r': { summary: 'saved', body: 'bb' } } } as any });
    mount();
    expect((screen.getByLabelText('Oggetto del commit') as HTMLInputElement).value).toBe('saved');
  });
});
