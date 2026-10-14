// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({
  Checkout: vi.fn(async (..._a: unknown[]) => undefined), CreateBranch: vi.fn(async (..._a: unknown[]) => undefined),
  CreateTag: vi.fn(async (..._a: unknown[]) => undefined), StashApply: vi.fn(async (..._a: unknown[]) => undefined),
  StashPop: vi.fn(async (..._a: unknown[]) => undefined), StashDrop: vi.fn(async (..._a: unknown[]) => undefined),
  Pull: vi.fn(async (..._a: unknown[]) => undefined), PullAutostash: vi.fn(async (..._a: unknown[]) => undefined),
  CommitDetail: vi.fn(async (..._a: unknown[]) => undefined as unknown), RevealInFinder: vi.fn(async (..._a: unknown[]) => undefined),
}));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { commitMenu, fileItems, localWrite, pull, splitUpstream, stashMenu } from './repoActions';
import { useRepo } from '../store/repo';
import { ContextMenuHost, openMenu, type MenuItem } from '../components/ContextMenu';
import { DialogHost } from '../components/Dialogs';

const uiErr = (kind: string) => new Error(JSON.stringify({ kind, message: kind, stderr: '' }));
const HASH = '0123456789abcdef0123456789abcdef01234567';

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockReset().mockResolvedValue(undefined));
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [], ops: {}, writes: {} });
});
afterEach(cleanup);

const mount = (items: MenuItem[]) => render(<>
  <button onContextMenu={(e) => openMenu(e, items)}>row</button><ContextMenuHost /><DialogHost />
</>);
const pick = async (item: string | RegExp) => {
  fireEvent.contextMenu(screen.getByText('row'));
  await act(async () => { fireEvent.click(screen.getByRole('menuitem', { name: item })); });
};
const type = (label: string | RegExp, v: string) => fireEvent.change(screen.getByLabelText(label), { target: { value: v } });
// Clicks the dialog's confirm (submit) button, whatever its action label.
const ok = async () => { await act(async () => { fireEvent.click(screen.getByRole('dialog').querySelector('button[type="submit"]')!); }); };

describe('commitMenu extras', () => {
  const clip = () => {
    const writeText = vi.fn(async (_t: string) => undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    return writeText;
  };
  it('copies the short hash and the full message', async () => {
    const writeText = clip();
    api.CommitDetail.mockResolvedValueOnce({ summary: 'Sum', body: 'Body line' });
    mount(commitMenu('t', HASH, 'Sum'));
    await pick('Copia hash breve');
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(HASH.slice(0, 7)));
    await pick('Copia messaggio');
    await waitFor(() => expect(writeText).toHaveBeenLastCalledWith('Sum\n\nBody line'));
    expect(api.CommitDetail).toHaveBeenCalledWith('t', HASH);
  });
  it('copy message falls back to the subject when the detail cannot be read', async () => {
    const writeText = clip();
    api.CommitDetail.mockRejectedValueOnce(new Error('x'));
    mount(commitMenu('t', HASH, 'Only subject'));
    await pick('Copia messaggio');
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('Only subject'));
  });
  it('"Mostra modifiche" selects the commit', async () => {
    useRepo.setState({ repos: { t: { stashes: [], graphRows: 0, graphDone: true, graphVersion: 0, selection: { kind: 'wip' }, diff: null } } });
    mount(commitMenu('t', HASH));
    await pick('Mostra modifiche');
    expect(useRepo.getState().repos.t.selection).toEqual({ kind: 'commit', hash: HASH });
  });
  it('stash: copy message', async () => {
    const writeText = clip();
    mount(stashMenu('t', { Hash: 'h', Ref: 'stash@{0}', Subject: 'On main: wip' }));
    await pick('Copia messaggio');
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('On main: wip'));
  });
  it('file items: copy path and reveal in Finder', async () => {
    const writeText = clip();
    mount(fileItems('t', 'dir/a.txt'));
    await pick('Copia percorso');
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('dir/a.txt'));
    await pick('Mostra nel Finder');
    expect(api.RevealInFinder).toHaveBeenCalledWith('t', 'dir/a.txt');
  });
  it('splitUpstream matches the longest known remote', () => {
    expect(splitUpstream('origin/feat/x', ['origin'])).toEqual({ remote: 'origin', branch: 'feat/x' });
    expect(splitUpstream('team/a/main', ['team', 'team/a'])).toEqual({ remote: 'team/a', branch: 'main' });
    expect(splitUpstream('other/main', ['origin'])).toBeNull();
  });
});

describe('commitMenu', () => {
  it('checkout warns about detached HEAD, then checks out the commit', async () => {
    mount(commitMenu('t', HASH));
    await pick('Checkout di questo commit');
    expect(api.Checkout).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog').textContent).toMatch(/HEAD/);
    await ok();
    await waitFor(() => expect(api.Checkout).toHaveBeenCalledWith('t', { kind: 'commit', name: HASH }));
  });
  it('checkout: cancel does nothing', async () => {
    mount(commitMenu('t', HASH));
    await pick('Checkout di questo commit');
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Annulla' })); });
    expect(api.Checkout).not.toHaveBeenCalled();
  });
  it('create branch here at the commit, with optional checkout', async () => {
    mount(commitMenu('t', HASH));
    await pick('Crea branch qui…');
    type('Nome', 'feat');
    expect((screen.getByRole('checkbox', { name: /checkout/i }) as HTMLInputElement).checked).toBe(true);
    await ok();
    await waitFor(() => expect(api.CreateBranch).toHaveBeenCalledWith('t', 'feat', HASH, true));
    await pick('Crea branch qui…');
    type('Nome', 'other');
    fireEvent.click(screen.getByRole('checkbox', { name: /checkout/i }));
    await ok();
    await waitFor(() => expect(api.CreateBranch).toHaveBeenLastCalledWith('t', 'other', HASH, false));
  });
  it('create tag: lightweight without message, annotated with one', async () => {
    mount(commitMenu('t', HASH));
    await pick('Crea tag qui…');
    type('Nome', 'v1');
    await ok();
    await screen.findByLabelText(/Messaggio/);
    await ok(); // empty message allowed
    await waitFor(() => expect(api.CreateTag).toHaveBeenCalledWith('t', 'v1', HASH, ''));
    await pick('Crea tag qui…');
    type('Nome', 'v2');
    await ok();
    type(/Messaggio/, 'release 2');
    await ok();
    await waitFor(() => expect(api.CreateTag).toHaveBeenLastCalledWith('t', 'v2', HASH, 'release 2'));
  });
  it('create tag: cancelling the message dialog creates nothing', async () => {
    mount(commitMenu('t', HASH));
    await pick('Crea tag qui…');
    type('Nome', 'v1');
    await ok();
    await screen.findByLabelText(/Messaggio/);
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Annulla' })); });
    expect(api.CreateTag).not.toHaveBeenCalled();
  });
  it('copies the full hash', async () => {
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    mount(commitMenu('t', HASH));
    await pick('Copia hash');
    expect(writeText).toHaveBeenCalledWith(HASH);
    // The menu is gone: a short success toast confirms the copy.
    await waitFor(() => expect(useApp.getState().toasts[0]).toMatchObject({ severity: 'success', title: 'Copiato ✓', tab: 't' }));
  });
  it('dialogs name their action on the confirm button', async () => {
    mount(commitMenu('t', HASH));
    await pick('Crea branch qui…');
    expect(screen.getByRole('button', { name: 'Crea branch' })).toBeTruthy();
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Annulla' })); });
    await pick('Checkout di questo commit');
    expect(screen.getByRole('button', { name: 'Checkout' })).toBeTruthy();
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Annulla' })); });
    await pick('Crea tag qui…');
    type('Nome', 'v1');
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Avanti' })); });
    expect(await screen.findByRole('button', { name: 'Crea tag' })).toBeTruthy();
  });
  it('a failing write becomes a toast and clears the busy counter', async () => {
    api.Checkout.mockRejectedValueOnce(uiErr('DirtyWorktree'));
    mount(commitMenu('t', HASH));
    await pick('Checkout di questo commit');
    await ok();
    await waitFor(() => expect(useApp.getState().toasts).toHaveLength(1));
    expect(useApp.getState().writes.t).toBeUndefined();
  });
});

describe('stashMenu', () => {
  it('apply/pop by hash; drop only after confirmation', async () => {
    mount(stashMenu('t', { Hash: 'sh', Ref: 'stash@{0}', Subject: 'wip' } as never));
    await pick('Applica');
    await pick('Pop');
    expect(api.StashApply).toHaveBeenCalledWith('t', 'sh');
    expect(api.StashPop).toHaveBeenCalledWith('t', 'sh');
    await pick('Elimina');
    expect(api.StashDrop).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog').textContent).toContain('stash@{0}');
    await ok();
    await waitFor(() => expect(api.StashDrop).toHaveBeenCalledWith('t', 'sh'));
  });
});

describe('localWrite', () => {
  it('marks the tab busy while the write runs', async () => {
    let done!: () => void;
    const p = localWrite('t', new Promise<void>((r) => { done = r; }));
    expect(useApp.getState().writes.t).toBe(1);
    done();
    await p;
    expect(useApp.getState().writes.t).toBeUndefined();
  });
});

describe('pull', () => {
  it('PullStrategyRequired asks for a strategy and retries with it', async () => {
    api.Pull.mockRejectedValueOnce(uiErr('PullStrategyRequired'));
    render(<DialogHost />);
    let p!: Promise<void>;
    act(() => { p = pull('t'); });
    await screen.findByRole('dialog');
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Solo fast-forward' })); });
    await p;
    expect(api.Pull.mock.calls).toEqual([['t', ''], ['t', 'ff-only']]);
  });
  it('DirtyWorktree offers autostash and pulls through runOp with the same mode', async () => {
    api.Pull.mockRejectedValueOnce(uiErr('DirtyWorktree'));
    let release!: () => void;
    api.PullAutostash.mockImplementationOnce(() => new Promise<undefined>((r) => { release = () => r(undefined); }));
    render(<DialogHost />);
    let p!: Promise<void>;
    act(() => { p = pull('t', 'rebase'); });
    await screen.findByRole('dialog');
    expect(api.PullAutostash).not.toHaveBeenCalled();
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Metti da parte le modifiche, fai pull e riapplica' })); });
    await waitFor(() => expect(api.PullAutostash).toHaveBeenCalledWith('t', 'rebase'));
    expect(useApp.getState().ops.t?.op).toBe('pull'); // in flight through runOp
    release();
    await p;
    expect(useApp.getState().ops.t).toBeFalsy();
    expect(useApp.getState().toasts.map((t) => [t.severity, t.title])).toEqual([['success', 'Pull completato']]);
  });
  it('DirtyWorktree: cancelling the choice does nothing more', async () => {
    api.Pull.mockRejectedValueOnce(uiErr('DirtyWorktree'));
    render(<DialogHost />);
    let p!: Promise<void>;
    act(() => { p = pull('t'); });
    await screen.findByRole('dialog');
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Annulla' })); });
    await p;
    expect(api.PullAutostash).not.toHaveBeenCalled();
    expect(useApp.getState().toasts).toHaveLength(0);
  });
  it('UntrackedWouldBeOverwritten never offers autostash: it is a toast', async () => {
    api.Pull.mockRejectedValueOnce(uiErr('UntrackedWouldBeOverwritten'));
    render(<DialogHost />);
    await act(async () => { await pull('t'); });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(api.PullAutostash).not.toHaveBeenCalled();
    expect(useApp.getState().toasts.map((t) => t.error.kind)).toEqual(['UntrackedWouldBeOverwritten']);
  });
  it('a successful pull shows a success notice that goes away by itself', async () => {
    await act(async () => { await pull('t'); });
    const [t] = useApp.getState().toasts;
    expect([t.severity, t.title]).toEqual(['success', 'Pull completato']);
  });
  it('an autostash conflict becomes a toast', async () => {
    api.Pull.mockRejectedValueOnce(uiErr('DirtyWorktree'));
    api.PullAutostash.mockRejectedValueOnce(uiErr('AutostashConflict'));
    render(<DialogHost />);
    let p!: Promise<void>;
    act(() => { p = pull('t'); });
    await screen.findByRole('dialog');
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /Metti da parte/ })); });
    await p;
    expect(useApp.getState().toasts.map((t) => t.error.kind)).toEqual(['AutostashConflict']);
  });
});
