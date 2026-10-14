// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({
  StashApply: vi.fn(async () => undefined), StashPop: vi.fn(async () => undefined), StashDrop: vi.fn(async () => undefined),
  DeleteBranch: vi.fn(async (..._a: unknown[]) => undefined), Checkout: vi.fn(async () => undefined),
  DeleteRemoteBranch: vi.fn(async () => undefined), PushTag: vi.fn(async () => undefined),
  FindRow: vi.fn(async (..._a: unknown[]) => 7),
  Pull: vi.fn(async (..._a: unknown[]) => undefined), Push: vi.fn(async (..._a: unknown[]) => undefined),
  CreateBranch: vi.fn(async (..._a: unknown[]) => undefined), DeleteTag: vi.fn(async (..._a: unknown[]) => undefined),
}));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { LeftPanel } from './LeftPanel';
import { ContextMenuHost } from './ContextMenu';
import { DialogHost } from './Dialogs';

const ref = (name: string, short: string, kind: string, remote = '', extra = {}) =>
  ({ name, short, kind, remote, hash: 'h' + short, upstream: '', ahead: 0, behind: 0, gone: false, isHead: false, ...extra }) as any;

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockClear());
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [], ops: {} });
  useRepo.setState({ repos: { t: {
    stashes: [{ Hash: 'abc123', Ref: 'stash@{0}', Subject: 'wip on main' }] as any,
    refs: { head: { branch: 'main', detached: false, unborn: false, hash: 'h' }, refs: [
      ref('refs/heads/main', 'main', 'local', '', { isHead: true, upstream: 'origin/main' }),
      ref('refs/heads/feat/x', 'feat/x', 'local', '', { upstream: 'origin/feat/x' }),
      ref('refs/remotes/origin/feat/y', 'origin/feat/y', 'remote', 'origin'), ref('refs/tags/v1', 'v1', 'tag'),
    ] } as any,
    graphRows: 0, graphDone: true, graphVersion: 0, selection: { kind: 'wip' }, diff: null,
  } } });
});
afterEach(cleanup);

const mount = () => render(<><LeftPanel tab="t" /><ContextMenuHost /><DialogHost /></>);
const menuItem = (row: string, item: string | RegExp) => {
  fireEvent.contextMenu(screen.getByText(row));
  fireEvent.click(screen.getByRole('menuitem', { name: item }));
};

describe('LeftPanel', () => {
  it('stash actions pass the stash hash, not the ref name', async () => {
    mount();
    menuItem('wip on main', 'Applica');
    menuItem('wip on main', 'Pop');
    expect(api.StashApply).toHaveBeenCalledWith('t', 'abc123');
    expect(api.StashPop).toHaveBeenCalledWith('t', 'abc123');
    menuItem('wip on main', 'Elimina');
    expect(api.StashDrop).not.toHaveBeenCalled(); // needs confirmation first
    expect(screen.getByRole('dialog').textContent).toContain('stash@{0}');
    fireEvent.click(screen.getByRole('button', { name: 'Elimina stash' }));
    await waitFor(() => expect(api.StashDrop).toHaveBeenCalledWith('t', 'abc123'));
  });
  it('delete branch asks for confirmation; force only after a "not fully merged" failure', async () => {
    api.DeleteBranch.mockRejectedValueOnce(JSON.stringify({ kind: 'Unknown', message: "error: the branch 'feat/x' is not fully merged", stderr: '' }));
    mount();
    menuItem('feat/x', 'Elimina feat/x');
    expect(api.DeleteBranch).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Elimina branch' }));
    await waitFor(() => expect(api.DeleteBranch).toHaveBeenCalledWith('t', 'feat/x', false));
    await waitFor(() => expect(screen.getByRole('dialog').textContent).toContain('non è completamente unito'));
    fireEvent.click(screen.getByRole('button', { name: 'Forza eliminazione' }));
    await waitFor(() => expect(api.DeleteBranch).toHaveBeenCalledWith('t', 'feat/x', true));
  });
  it('other delete failures toast and never force', async () => {
    api.DeleteBranch.mockRejectedValueOnce(JSON.stringify({ kind: 'Locked', message: 'locked', stderr: '' }));
    mount();
    menuItem('feat/x', 'Elimina feat/x');
    fireEvent.click(screen.getByRole('button', { name: 'Elimina branch' }));
    await waitFor(() => expect(useApp.getState().toasts.length).toBe(1));
    expect(api.DeleteBranch).toHaveBeenCalledTimes(1);
  });
  it('the current branch cannot be deleted', () => {
    mount();
    fireEvent.contextMenu(screen.getByText('main'));
    expect(screen.getByRole('menuitem', { name: 'Elimina main' }).getAttribute('aria-disabled')).toBe('true');
  });
  it('remote branch delete uses the branch name without the remote prefix, via runOp', async () => {
    mount();
    menuItem('origin/feat/y', /Elimina origin\/feat\/y/);
    fireEvent.click(screen.getByRole('button', { name: 'Elimina dal remote' }));
    await waitFor(() => expect(api.DeleteRemoteBranch).toHaveBeenCalledWith('t', 'origin', 'feat/y'));
  });
  it('tag push goes to a known remote', async () => {
    mount();
    menuItem('v1', 'Push su origin');
    await waitFor(() => expect(api.PushTag).toHaveBeenCalledWith('t', 'origin', 'v1'));
  });
  it('double click checks out; single click jumps to the commit', async () => {
    mount();
    fireEvent.doubleClick(screen.getByText('feat/x'));
    expect(api.Checkout).toHaveBeenCalledWith('t', { kind: 'local', name: 'feat/x' });
    fireEvent.click(screen.getByText('feat/x'));
    await waitFor(() => expect(useRepo.getState().repos.t.selection).toEqual({ kind: 'commit', hash: 'hfeat/x' }));
    expect(useRepo.getState().repos.t.scrollRequest?.row).toBe(7);
  });
  it('filters by name', () => {
    mount();
    fireEvent.change(screen.getByLabelText('Filtra riferimenti'), { target: { value: 'feat' } });
    expect(screen.queryByText('v1')).toBeNull();
    expect(screen.getByText('feat/x')).toBeTruthy();
  });
  it('shows a detached HEAD entry', () => {
    useRepo.setState((s) => ({ repos: { t: { ...s.repos.t, refs: { head: { branch: '', detached: true, unborn: false, hash: 'abcdef1234' }, refs: [] } as any } } }));
    mount();
    expect(screen.getByText('HEAD scollegato')).toBeTruthy();
  });
});

const labels = () => screen.getAllByRole('menuitem').map((m) => m.textContent);
const separators = () => screen.getByRole('menu').querySelectorAll('[role="separator"]').length;

describe('LeftPanel rich menus', () => {
  it('current branch: checkout disabled with a reason, pull and push, no delete', async () => {
    mount();
    fireEvent.contextMenu(screen.getByText('main'));
    expect(labels()).toEqual(['Checkout main', 'Pull', 'Push', 'Imposta upstream…', 'Crea branch da qui…', 'Crea tag qui…',
      'Rinomina…', 'Copia nome del branch', 'Elimina main', 'Elimina main anche su origin…']);
    expect(separators()).toBeGreaterThanOrEqual(4);
    const co = screen.getByRole('menuitem', { name: 'Checkout main' });
    expect(co.getAttribute('aria-disabled')).toBe('true');
    expect(co.getAttribute('title')).toMatch(/corrente/);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Push' }));
    await waitFor(() => expect(api.Push).toHaveBeenCalledWith('t', false));
    await waitFor(() => expect(useApp.getState().toasts.map((t) => t.title)).toEqual(['Push completato']));
  });
  it('another branch: no pull/push (the backend pushes the current branch only)', () => {
    mount();
    fireEvent.contextMenu(screen.getByText('feat/x'));
    expect(labels()).not.toContain('Pull');
    expect(labels()).not.toContain('Push');
  });
  it('delete also on origin names both, deletes local then remote', async () => {
    mount();
    menuItem('feat/x', 'Elimina feat/x anche su origin…');
    const dlg = screen.getByRole('dialog').textContent!;
    expect(dlg).toContain('feat/x');
    expect(dlg).toContain('origin');
    fireEvent.click(screen.getByRole('button', { name: 'Elimina entrambi' }));
    await waitFor(() => expect(api.DeleteRemoteBranch).toHaveBeenCalledWith('t', 'origin', 'feat/x'));
    expect(api.DeleteBranch).toHaveBeenCalledWith('t', 'feat/x', false);
  });
  it('delete also on origin: a failed local delete leaves the remote alone', async () => {
    api.DeleteBranch.mockRejectedValueOnce(JSON.stringify({ kind: 'Locked', message: 'locked', stderr: '' }));
    mount();
    menuItem('feat/x', 'Elimina feat/x anche su origin…');
    fireEvent.click(screen.getByRole('button', { name: 'Elimina entrambi' }));
    await waitFor(() => expect(useApp.getState().toasts.length).toBe(1));
    expect(api.DeleteRemoteBranch).not.toHaveBeenCalled();
  });
  it('copy branch name', async () => {
    const writeText = vi.fn(async () => undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    mount();
    menuItem('feat/x', 'Copia nome del branch');
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('feat/x'));
  });
  it('remote branch: create a local branch from it, named like it', async () => {
    mount();
    menuItem('origin/feat/y', 'Crea branch locale da qui…');
    expect((screen.getByLabelText('Nome') as HTMLInputElement).value).toBe('feat/y');
    fireEvent.submit(screen.getByRole('dialog'));
    await waitFor(() => expect(api.CreateBranch).toHaveBeenCalledWith('t', 'feat/y', 'refs/remotes/origin/feat/y', true));
  });
  it('tag: detached checkout after confirmation; local delete only', async () => {
    mount();
    fireEvent.contextMenu(screen.getByText('v1'));
    expect(labels()).toEqual(['Checkout v1…', 'Push su origin', 'Copia nome del tag', 'Elimina v1']);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Checkout v1…' }));
    expect(api.Checkout).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Checkout' }));
    await waitFor(() => expect(api.Checkout).toHaveBeenCalledWith('t', { kind: 'commit', name: 'hv1' }));
  });
  it('the menu key opens a row menu from the keyboard', () => {
    mount();
    fireEvent.keyDown(screen.getByText('feat/x').closest('li')!, { key: 'F10', shiftKey: true });
    expect(screen.getByRole('menu')).toBeTruthy();
  });
});

describe('LeftPanel ahead/behind', () => {
  const withCounts = (ahead: number, behind: number) => useRepo.setState((s) => ({ repos: { t: { ...s.repos.t,
    refs: { head: { branch: 'main', detached: false, unborn: false, hash: 'h' }, refs: [ref('refs/heads/feat/x', 'feat/x', 'local', '', { ahead, behind })] } as any } } }));
  const row = () => screen.getByText('feat/x').closest('li')!;
  it('shows only the non-zero counts, as badges', () => {
    withCounts(2, 0);
    mount();
    expect(row().querySelectorAll('.badge')).toHaveLength(1);
    expect(screen.getByText('2 commit da inviare').closest('.badge')!.textContent).toContain('↑2');
    expect(row().textContent).not.toContain('↓');
  });
  it('shows both when the branch diverged, and nothing when in sync', () => {
    withCounts(1, 3);
    const { unmount } = mount();
    expect(screen.getByText('3 commit da ricevere').closest('.badge')!.textContent).toContain('↓3');
    expect(row().querySelectorAll('.badge')).toHaveLength(2);
    unmount();
    withCounts(0, 0);
    mount();
    expect(row().querySelectorAll('.badge')).toHaveLength(0);
  });
});
