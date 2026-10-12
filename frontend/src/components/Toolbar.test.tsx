// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({
  Fetch: vi.fn(async (..._a: unknown[]) => undefined), Pull: vi.fn(async (..._a: unknown[]) => undefined),
  Push: vi.fn(async (..._a: unknown[]) => undefined), CancelOp: vi.fn(async (..._a: unknown[]) => undefined),
  CreateBranch: vi.fn(async (..._a: unknown[]) => undefined), StashPush: vi.fn(async (..._a: unknown[]) => undefined),
  StashPop: vi.fn(async (..._a: unknown[]) => undefined),
}));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { Toolbar } from './Toolbar';
import { DialogHost } from './Dialogs';
import { ContextMenuHost } from './ContextMenu';
import { Toasts } from './Toasts';

const uiErr = (kind: string) => new Error(JSON.stringify({ kind, message: kind, stderr: '' }));

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockReset().mockResolvedValue(undefined));
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [], ops: {} });
  useRepo.setState({ repos: { t: { stashes: [{ Hash: 'h1' } as any], graphRows: 0, graphDone: true, graphVersion: 0,
    selection: { kind: 'wip' }, diff: null, refs: { head: { branch: 'main', detached: false, unborn: false, hash: 'abc1234' }, refs: [] } as any } } });
});
afterEach(cleanup);

const mount = () => render(<><Toolbar tab="t" /><DialogHost /><ContextMenuHost /><Toasts /></>);

describe('Toolbar', () => {
  it('fetch runs through runOp: op visible while running, cleared afterwards', async () => {
    let done!: () => void;
    api.Fetch.mockImplementation(() => new Promise<undefined>((r) => { done = () => r(undefined); }));
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));
    await waitFor(() => expect(useApp.getState().ops.t?.op).toBe('fetch'));
    expect(screen.getByRole('button', { name: 'Annulla operazione' })).toBeTruthy();
    await act(async () => { done(); });
    expect(useApp.getState().ops.t).toBeUndefined();
    expect(api.Fetch).toHaveBeenCalledWith('t');
  });
  it('shows progress and cancels the running op', async () => {
    let done!: () => void;
    api.Fetch.mockImplementation(() => new Promise<undefined>((r) => { done = () => r(undefined); }));
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));
    await waitFor(() => expect(useApp.getState().ops.t).toBeTruthy());
    act(() => useApp.getState().progressOp('t', { op: 'fetch', phase: 'Receiving objects', percent: 42 }));
    expect(screen.getByRole('status').textContent).toContain('42%');
    fireEvent.click(screen.getByRole('button', { name: 'Annulla operazione' }));
    expect(api.CancelOp).toHaveBeenCalledWith('t');
    await act(async () => { done(); }); // settle runOp so the module-level counter is released
  });
  it('pull: PullStrategyRequired offers merge/rebase/ff-only and retries with the choice', async () => {
    api.Pull.mockRejectedValueOnce(uiErr('PullStrategyRequired'));
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Pull' }));
    const dlg = await screen.findByRole('dialog');
    expect(dlg.textContent).toContain('Merge');
    expect(dlg.textContent).toContain('Rebase');
    expect(dlg.textContent).toContain('fast-forward');
    expect(useApp.getState().toasts).toHaveLength(0);
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Rebase' })); });
    await waitFor(() => expect(api.Pull).toHaveBeenLastCalledWith('t', 'rebase'));
    expect(api.Pull).toHaveBeenCalledTimes(2);
    expect(api.Pull.mock.calls[0]).toEqual(['t', '']);
  });
  it('pull: cancelling the strategy choice does not retry nor toast', async () => {
    api.Pull.mockRejectedValueOnce(uiErr('PullStrategyRequired'));
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Pull' }));
    await screen.findByRole('dialog');
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Annulla' })); });
    expect(api.Pull).toHaveBeenCalledTimes(1);
    expect(useApp.getState().toasts).toHaveLength(0);
  });
  it('pull: other errors become toasts; Cancelled stays silent', async () => {
    api.Pull.mockRejectedValueOnce(uiErr('Network')).mockRejectedValueOnce(uiErr('Cancelled'));
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Pull' }));
    await waitFor(() => expect(useApp.getState().toasts).toHaveLength(1));
    fireEvent.click(screen.getByRole('button', { name: 'Pull' }));
    await waitFor(() => expect(api.Pull).toHaveBeenCalledTimes(2));
    expect(useApp.getState().toasts).toHaveLength(1);
  });
  it('force push needs an explicit confirm', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Altre opzioni push' }));
    await act(async () => { fireEvent.click(await screen.findByRole('menuitem', { name: /Force push/ })); });
    const dlg = await screen.findByRole('dialog');
    expect(api.Push).not.toHaveBeenCalled();
    await act(async () => { fireEvent.click(dlg.querySelector('button.danger')!); });
    await waitFor(() => expect(api.Push).toHaveBeenCalledWith('t', true));
  });
  it('plain push is not forced; busy toolbar blocks a second remote op', async () => {
    let done!: () => void;
    api.Push.mockImplementation(() => new Promise<undefined>((r) => { done = () => r(undefined); }));
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Push' }));
    await waitFor(() => expect(api.Push).toHaveBeenCalledWith('t', false));
    await waitFor(() => expect((screen.getByRole('button', { name: 'Fetch' }) as HTMLButtonElement).disabled).toBe(true));
    await act(async () => { done(); });
  });
  it('stash pops the newest stash by hash', async () => {
    mount();
    fireEvent.click(screen.getByRole('button', { name: 'Pop stash' }));
    await waitFor(() => expect(api.StashPop).toHaveBeenCalledWith('t', 'h1'));
    fireEvent.click(screen.getByRole('button', { name: 'Stash' }));
    await waitFor(() => expect(api.StashPush).toHaveBeenCalledWith('t', '', true));
  });
  it('Stash and Branch ignore a second activation while one is running', async () => {
    let done!: () => void;
    api.StashPush.mockImplementation(() => new Promise<undefined>((r) => { done = () => r(undefined); }));
    mount();
    const b = screen.getByRole('button', { name: 'Stash' });
    fireEvent.click(b);
    fireEvent.click(b);
    expect(api.StashPush).toHaveBeenCalledTimes(1);
    await act(async () => { done(); });
    fireEvent.click(b);
    expect(api.StashPush).toHaveBeenCalledTimes(2);
    await act(async () => { done(); });
  });
  it('Branch dialog cannot be opened twice', async () => {
    mount();
    const b = screen.getByRole('button', { name: 'Branch' });
    fireEvent.click(b);
    fireEvent.click(b);
    expect(await screen.findAllByRole('dialog')).toHaveLength(1);
  });
});
