import { beforeEach, describe, expect, it } from 'vitest';
import { useApp } from './app';

beforeEach(() => useApp.setState({ toasts: [], authQueue: [] }));

describe('useApp', () => {
  it('does not toast Cancelled errors', () => {
    useApp.getState().toast('{"kind":"Cancelled","message":"","stderr":""}');
    expect(useApp.getState().toasts).toHaveLength(0);
    useApp.getState().toast('{"kind":"Timeout","message":"","stderr":""}', 't1');
    expect(useApp.getState().toasts).toHaveLength(1);
  });
  it('removes a cancelled auth prompt by id', () => {
    const { pushAuth, cancelAuth } = useApp.getState();
    pushAuth({ id: 'a', prompt: 'Password:', secret: true });
    pushAuth({ id: 'b', prompt: 'Username:', secret: false });
    cancelAuth('a');
    expect(useApp.getState().authQueue.map((p) => p.id)).toEqual(['b']);
  });
});

// --- Fix round 1 ---
import { vi } from 'vitest';
import { api } from '../api/client';
import { useRepo } from './repo';
import { parseError } from '../api/client';
import { displayName } from '../api/paths';

vi.mock('../api/client', async (orig) => {
  const m = await orig<typeof import('../api/client')>();
  return {
    ...m,
    api: {
      CloseTab: vi.fn(async () => undefined),
      GetState: vi.fn(async () => ({ activeTab: '', recents: [], favorites: [] })),
      RestoreTabs: vi.fn(async () => []),
      StartupError: vi.fn(async () => ''),
      GraphInfo: vi.fn(async () => ({ rows: 0, done: false })),
      Status: vi.fn(async () => ({})),
      Refs: vi.fn(async () => ({})),
      Stashes: vi.fn(async () => []),
    },
  };
});

const tab = (id: string) => ({ id, path: `/r/${id}`, name: id });
const mocked = api as unknown as Record<string, ReturnType<typeof vi.fn>>;

beforeEach(() => {
  useApp.setState({ tabs: [], active: null, ops: {}, toasts: [], authQueue: [] });
  useRepo.setState({ repos: {} });
});

describe('tab cleanup', () => {
  it('close drops repo state, op and toasts of the tab', async () => {
    useApp.setState({ tabs: [tab('a'), tab('b')], active: 'a' });
    useRepo.getState().patch('a', { graphRows: 5 });
    useRepo.getState().patch('b', { graphRows: 7 });
    useApp.getState().setOp('a', { op: 'pull', phase: '', percent: 0 });
    useApp.getState().toast('boom', 'a');
    useApp.getState().toast('boom', 'b');
    await useApp.getState().close('a');
    expect(useRepo.getState().repos.a).toBeUndefined();
    expect(useRepo.getState().repos.b).toBeDefined();
    expect(useApp.getState().ops.a).toBeUndefined();
    expect(useApp.getState().toasts.map((t) => t.tab)).toEqual(['b']);
  });
  it('ignores patches, refreshes and progress for tabs that are not open', async () => {
    useRepo.getState().patch('gone', { graphRows: 3 });
    await useRepo.getState().refresh('gone');
    await useRepo.getState().refreshStatus('gone');
    await useRepo.getState().refreshRefs('gone');
    expect(useRepo.getState().repos.gone).toBeUndefined();
    useApp.getState().progressOp('gone', { op: 'pull', phase: '', percent: 1 });
    expect(useApp.getState().ops.gone).toBeUndefined();
  });
});

describe('init', () => {
  it('keeps tabs opened while RestoreTabs runs and does not override active', async () => {
    mocked.RestoreTabs.mockImplementationOnce(async () => {
      useApp.getState().addTab(tab('new'));
      return [tab('old'), tab('new')];
    });
    mocked.GetState.mockResolvedValueOnce({ activeTab: 'old', recents: [], favorites: [] });
    await useApp.getState().init();
    expect(useApp.getState().tabs.map((t) => t.id)).toEqual(['old', 'new']);
    expect(useApp.getState().active).toBe('new');
  });
  it('sets active from state when none is set yet', async () => {
    mocked.RestoreTabs.mockResolvedValueOnce([tab('x'), tab('y')]);
    mocked.GetState.mockResolvedValueOnce({ activeTab: 'y', recents: [], favorites: [] });
    await useApp.getState().init();
    expect(useApp.getState().active).toBe('y');
  });
});

describe('runOp', () => {
  it('sets then clears ops on success and on error', async () => {
    useApp.setState({ tabs: [tab('a')] });
    let during: unknown;
    await useApp.getState().runOp('a', 'pull', async () => { during = useApp.getState().ops.a; });
    expect(during).toMatchObject({ op: 'pull' });
    expect(useApp.getState().ops.a).toBeUndefined();
    await expect(useApp.getState().runOp('a', 'push', async () => { throw new Error('x'); })).rejects.toThrow('x');
    expect(useApp.getState().ops.a).toBeUndefined();
  });
  it('late progress after the op finished does not resurrect it', async () => {
    useApp.setState({ tabs: [tab('a')] });
    await useApp.getState().runOp('a', 'pull', async () => {
      useApp.getState().progressOp('a', { op: 'pull', phase: 'x', percent: 50 });
      expect(useApp.getState().ops.a?.percent).toBe(50);
    });
    useApp.getState().progressOp('a', { op: 'pull', phase: 'x', percent: 99 });
    expect(useApp.getState().ops.a).toBeUndefined();
  });
});

describe('graph refresh', () => {
  it('never regresses rows/done for the same graph version', async () => {
    useApp.setState({ tabs: [tab('a')] });
    mocked.GraphInfo.mockImplementationOnce(async () => {
      useRepo.getState().graphProgress('a', 100, true); // events arrive while the call is in flight
      return { rows: 40, done: false };
    });
    await useRepo.getState().refresh('a');
    expect(useRepo.getState().get('a')).toMatchObject({ graphRows: 100, graphDone: true });
  });
  it('drops a snapshot taken before a reset', async () => {
    useApp.setState({ tabs: [tab('a')] });
    mocked.GraphInfo.mockImplementationOnce(async () => {
      useRepo.getState().graphReset('a');
      return { rows: 40, done: true };
    });
    await useRepo.getState().refresh('a');
    expect(useRepo.getState().get('a')).toMatchObject({ graphRows: 0, graphDone: false });
  });
});

describe('helpers', () => {
  it('parseError stringifies plain objects', () => {
    expect(parseError({ a: 1 }).message).toBe('{"a":1}');
    expect(parseError({ kind: 'Timeout', message: 'm' }).kind).toBe('Timeout'); // structured objects round-trip via JSON
  });
  it('displayName handles both separators', () => {
    expect(displayName('/a/b/c')).toBe('c');
    expect(displayName('C:\\a\\b\\')).toBe('b');
    expect(displayName('/')).toBe('/');
  });
});
