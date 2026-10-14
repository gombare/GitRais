// @vitest-environment jsdom
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const row = (hash: string, kind = 'commit', extra = {}) => ({
  hash, parents: [], kind, lane: 0, color: 0, up: [], down: [], author: 'a', email: '', time: 0,
  subject: `subj ${hash}`, stashRef: '', ...extra,
});
const api = vi.hoisted(() => ({
  Rows: vi.fn(async (..._a: unknown[]) => [] as unknown[]),
  Checkout: vi.fn(async (..._a: unknown[]) => undefined),
  StashApply: vi.fn(async (..._a: unknown[]) => undefined),
}));
vi.mock('../hooks/useDevicePixelRatio', () => ({ useDevicePixelRatio: () => 1 }));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { GraphView } from './GraphView';
import { ContextMenuHost } from './ContextMenu';
import { DialogHost } from './Dialogs';

beforeAll(() => {
  globalThis.ResizeObserver ??= class { observe() {} disconnect() {} unobserve() {} } as never;
  HTMLCanvasElement.prototype.getContext = (() => null) as never;
});
beforeEach(() => {
  api.Rows.mockReset().mockResolvedValue([row('c1'), row('s1', 'stash', { stashRef: 'stash@{0}', subject: 'On main: wip' })]);
  api.Checkout.mockClear();
  api.StashApply.mockClear();
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [], ops: {}, writes: {} });
  useRepo.setState({ repos: { t: { stashes: [], graphRows: 2, graphDone: true, graphVersion: 0,
    selection: { kind: 'wip' }, diff: null } as never } });
});
afterEach(() => {
  fireEvent.mouseDown(window); // the menu store is module-level: close any menu left open
  cleanup();
});

const mount = () => render(<><GraphView tab="t" /><ContextMenuHost /><DialogHost /></>);

describe('GraphView context menu', () => {
  it('right click on a commit row offers the commit actions and selects the row', async () => {
    mount();
    fireEvent.contextMenu(await screen.findByText('subj c1'));
    expect(screen.getByRole('menuitem', { name: 'Checkout di questo commit' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Crea branch qui…' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Crea tag qui…' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Copia hash' })).toBeTruthy();
    expect(useRepo.getState().repos.t.selection).toEqual({ kind: 'commit', hash: 'c1' });
  });
  it('right click on a stash row offers apply/pop/drop by hash', async () => {
    mount();
    fireEvent.contextMenu(await screen.findByText('On main: wip'));
    await act(async () => { fireEvent.click(screen.getByRole('menuitem', { name: 'Applica' })); });
    await waitFor(() => expect(api.StashApply).toHaveBeenCalledWith('t', 's1'));
    fireEvent.contextMenu(screen.getByText('On main: wip'));
    expect(screen.getByRole('menuitem', { name: 'Pop' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Elimina' })).toBeTruthy();
  });
  it('Shift+F10 and the ContextMenu key open the menu of the selected row; focus returns to the list', async () => {
    mount();
    fireEvent.click(await screen.findByText('subj c1'));
    const list = screen.getByRole('listbox', { name: 'Storia dei commit' });
    list.focus();
    fireEvent.keyDown(list, { key: 'F10', shiftKey: true });
    // The first item gets the focus.
    const item = screen.getByRole('menuitem', { name: 'Mostra modifiche' });
    expect(document.activeElement).toBe(item);
    fireEvent.keyDown(item, { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(document.activeElement).toBe(list);
    fireEvent.keyDown(list, { key: 'ContextMenu' });
    expect(screen.getByRole('menu')).toBeTruthy();
  });
  it('the menu key does nothing without a selected row', async () => {
    mount();
    await screen.findByText('subj c1');
    fireEvent.keyDown(screen.getByRole('listbox'), { key: 'F10', shiftKey: true });
    expect(screen.queryByRole('menu')).toBeNull();
  });
});
