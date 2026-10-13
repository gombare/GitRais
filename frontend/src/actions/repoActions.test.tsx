// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({
  Checkout: vi.fn(async (..._a: unknown[]) => undefined), CreateBranch: vi.fn(async (..._a: unknown[]) => undefined),
  CreateTag: vi.fn(async (..._a: unknown[]) => undefined), StashApply: vi.fn(async (..._a: unknown[]) => undefined),
  StashPop: vi.fn(async (..._a: unknown[]) => undefined), StashDrop: vi.fn(async (..._a: unknown[]) => undefined),
  Pull: vi.fn(async (..._a: unknown[]) => undefined),
}));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { commitMenu, localWrite, pull, stashMenu } from './repoActions';
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
const ok = async () => { await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'OK' })); }); };

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
});
