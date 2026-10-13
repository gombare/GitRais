// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({
  SaveLayout: vi.fn(async (..._a: unknown[]) => undefined),
  StageAll: vi.fn(async (..._a: unknown[]) => undefined),
  UnstageAll: vi.fn(async (..._a: unknown[]) => undefined),
}));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));
vi.mock('./GraphView', () => ({ GraphView: () => null }));
vi.mock('./LeftPanel', () => ({ LeftPanel: () => null }));
vi.mock('./DetailPanel', () => ({ DetailPanel: () => null }));
vi.mock('./Toolbar', () => ({ Toolbar: () => <button data-command="commit:commit" onClick={() => commitClick()}>c</button>, useRepoActions: () => ({}) }));
const commitClick = vi.hoisted(() => vi.fn());

import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { RepoView } from './RepoView';

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockReset().mockResolvedValue(undefined));
  commitClick.mockReset();
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [], ops: {}, state: undefined } as never);
  useRepo.setState({ repos: { t: { stashes: [], graphRows: 0, graphDone: true, graphVersion: 0, selection: { kind: 'wip' }, diff: null } as never } });
  useRepo.setState({ refresh: vi.fn(async () => undefined), refreshStatus: vi.fn(async () => undefined) } as never);
});
afterEach(cleanup);

describe('RepoView', () => {
  it('keyboard resize saves the new clamped width, not the stale one', async () => {
    render(<RepoView tab="t" />);
    fireEvent.keyDown(screen.getByRole('separator', { name: 'Larghezza pannello sinistro' }), { key: 'ArrowRight' });
    expect(api.SaveLayout).toHaveBeenCalledTimes(1);
    expect(api.SaveLayout).toHaveBeenLastCalledWith({ leftWidth: 276, rightWidth: 340 });
    fireEvent.keyDown(screen.getByRole('separator', { name: 'Larghezza pannello dettagli' }), { key: 'End' });
    expect(api.SaveLayout).toHaveBeenLastCalledWith({ leftWidth: 276, rightWidth: 700 });
  });
  it('publishes the detail panel width for the toasts while active, and clears it otherwise', () => {
    const w = () => document.documentElement.style.getPropertyValue('--right-panel-w');
    const { unmount } = render(<RepoView tab="t" />);
    expect(w()).toBe('344px');
    fireEvent.keyDown(screen.getByRole('separator', { name: 'Larghezza pannello dettagli' }), { key: 'End' });
    expect(w()).toBe('704px');
    act(() => useApp.setState({ active: null }));
    expect(w()).toBe('');
    unmount();
  });
  it('commitAll aborts without committing when StageAll fails', async () => {
    api.StageAll.mockRejectedValue(new Error('boom'));
    render(<RepoView tab="t" />);
    fireEvent.keyDown(window, { key: 'Enter', metaKey: true, shiftKey: true });
    await waitFor(() => expect(api.StageAll).toHaveBeenCalled());
    await waitFor(() => expect(useApp.getState().toasts).toHaveLength(1));
    await new Promise((r) => setTimeout(r, 20));
    expect(commitClick).not.toHaveBeenCalled();
  });
  it('commitAll commits after a successful StageAll', async () => {
    render(<RepoView tab="t" />);
    fireEvent.keyDown(window, { key: 'Enter', metaKey: true, shiftKey: true });
    await waitFor(() => expect(commitClick).toHaveBeenCalled());
  });
});
