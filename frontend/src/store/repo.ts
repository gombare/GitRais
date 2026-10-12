import { create } from 'zustand';
import { api } from '../api/client';
import type { gitread, graph, status } from '../../wailsjs/go/models';
import { useApp } from './app';

export type Selection = { kind: 'wip' } | { kind: 'commit'; hash: string };
export type DiffTarget =
  // oldPath: pre-rename path of a staged rename (old side of the staged diff).
  | { kind: 'wip'; path: string; staged: boolean; untracked: boolean; oldPath?: string }
  | { kind: 'commit'; hash: string; path: string; oldPath: string };

export type RepoState = {
  refs?: gitread.Refs;
  status?: status.Status;
  stashes: graph.Stash[];
  graphRows: number;
  graphDone: boolean;
  graphVersion: number;
  selection: Selection;
  diff: DiffTarget | null;
  // Set by jump(): the graph scrolls to `row`; `n` makes repeated jumps to the same row distinct.
  scrollRequest?: { row: number; n: number };
};

const empty = (): RepoState => ({
  stashes: [], graphRows: 0, graphDone: false, graphVersion: 0, selection: { kind: 'wip' }, diff: null,
});

type RepoStore = {
  repos: Record<string, RepoState>;
  get(tab: string): RepoState;
  patch(tab: string, p: Partial<RepoState>): void;
  refresh(tab: string): Promise<void>;
  refreshStatus(tab: string): Promise<void>;
  refreshRefs(tab: string): Promise<void>;
  graphProgress(tab: string, rows: number, done: boolean): void;
  graphReset(tab: string): void;
  select(tab: string, sel: Selection): void;
  openDiff(tab: string, d: DiffTarget | null): void;
  // Selects the commit and asks the graph to scroll to it (selection only, no scroll, if the commit is not in the graph).
  jump(tab: string, hash: string): Promise<void>;
};

// Latest status request per tab: out-of-order responses (event storms) are dropped.
const statusSeq = new Map<string, number>();

// Drops the bookkeeping of a closed tab.
export const clearStatusSeq = (tab: string) => { statusSeq.delete(tab); };

export const useRepo = create<RepoStore>((set, get) => ({
  repos: {},
  get(tab) {
    return get().repos[tab] ?? empty();
  },
  patch(tab, p) {
    // Ignore tabs that are not open (late events/responses for a closed tab).
    if (!useApp.getState().tabs.some((t) => t.id === tab)) return;
    set((s) => ({ repos: { ...s.repos, [tab]: { ...(s.repos[tab] ?? empty()), ...p } } }));
  },
  async refresh(tab) {
    await Promise.all([get().refreshRefs(tab), get().refreshStatus(tab)]);
    const version = get().get(tab).graphVersion;
    try {
      const gi = await api.GraphInfo(tab);
      // Progress events may have advanced past this snapshot: never regress rows/done,
      // and drop the snapshot if the graph was reset meanwhile.
      const cur = get().get(tab);
      if (cur.graphVersion === version) {
        get().patch(tab, { graphRows: Math.max(cur.graphRows, gi.rows), graphDone: cur.graphDone || gi.done });
      }
    } catch (e) {
      useApp.getState().toast(e, tab);
    }
  },
  async refreshStatus(tab) {
    const seq = (statusSeq.get(tab) ?? 0) + 1;
    statusSeq.set(tab, seq);
    try {
      const status = await api.Status(tab);
      if (statusSeq.get(tab) === seq) get().patch(tab, { status });
    } catch (e) {
      useApp.getState().toast(e, tab);
    }
  },
  async refreshRefs(tab) {
    try {
      const [refs, stashes] = await Promise.all([api.Refs(tab), api.Stashes(tab)]);
      get().patch(tab, { refs, stashes });
    } catch (e) {
      useApp.getState().toast(e, tab);
    }
  },
  graphProgress(tab, rows, done) {
    get().patch(tab, { graphRows: rows, graphDone: done });
  },
  graphReset(tab) {
    get().patch(tab, { graphVersion: get().get(tab).graphVersion + 1, graphRows: 0, graphDone: false });
  },
  select(tab, selection) {
    get().patch(tab, { selection, diff: null });
  },
  openDiff(tab, diff) {
    get().patch(tab, { diff });
  },
  async jump(tab, hash) {
    let row = -1;
    try {
      row = await api.FindRow(tab, hash);
    } catch (e) {
      useApp.getState().toast(e, tab);
      return;
    }
    get().patch(tab, { selection: { kind: 'commit', hash }, diff: null, ...(row >= 0 ? { scrollRequest: { row, n: Date.now() } } : {}) });
  },
}));
