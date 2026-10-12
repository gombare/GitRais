import { create } from 'zustand';
import { api, parseError, type AuthPrompt, type UIError } from '../api/client';
import { isSilent } from '../api/errors';
import type { app, store } from '../../wailsjs/go/models';
import { useRepo } from './repo';

export type Toast = { id: number; tab?: string; error: UIError };
export type OpState = { op: string; phase: string; percent: number } | null;

type AppStore = {
  tabs: app.TabInfo[];
  active: string | null;
  state: store.State | null;
  // Newline-separated startup problems from the backend (may be non-fatal).
  startupError: string;
  toasts: Toast[];
  authQueue: AuthPrompt[];
  ops: Record<string, OpState>;
  init(): Promise<void>;
  open(path: string): Promise<void>;
  close(id: string): Promise<void>;
  activate(id: string): void;
  addTab(tab: app.TabInfo): void;
  reloadState(): Promise<void>;
  toast(e: unknown, tab?: string): void;
  dismiss(id: number): void;
  setOp(tab: string, op: OpState): void;
  // Applies an `op:progress` event: ignored unless the op was started through runOp on a live tab.
  progressOp(tab: string, op: NonNullable<OpState>): void;
  // Marks `op` as running on `tab` (use '' for tab-less ops such as clone) for the duration of fn.
  // ops[tab] is cleared when fn settles, on success or error; the error is rethrown.
  runOp<T>(tab: string, op: string, fn: () => Promise<T>): Promise<T>;
  pushAuth(p: AuthPrompt): void;
  shiftAuth(): void;
  cancelAuth(id: string): void;
};

let nextToast = 1;
// Running runOp calls per tab: ops[tab] is cleared only when the last one settles.
const activeOps = new Map<string, number>();

export const useApp = create<AppStore>((set, get) => ({
  tabs: [],
  active: null,
  state: null,
  startupError: '',
  toasts: [],
  authQueue: [],
  ops: {},
  async init() {
    // The app can run with startup problems (e.g. git missing): show them but keep going.
    try {
      set({ startupError: await api.StartupError() });
    } catch (e) {
      get().toast(e);
    }
    try {
      const restored = (await api.RestoreTabs()) ?? [];
      const state = await api.GetState();
      // Tabs opened by the user while RestoreTabs ran must survive: merge instead of replacing.
      set((s) => {
        const tabs = [...restored, ...s.tabs.filter((t) => !restored.some((r) => r.id === t.id))];
        const active = s.active ?? tabs.find((t) => t.id === state.activeTab)?.id ?? tabs[0]?.id ?? null;
        return { tabs, state, active };
      });
    } catch (e) {
      get().toast(e);
      // Welcome still needs the state (recents/favorites) even when the tabs failed to restore.
      try {
        set({ state: await api.GetState() });
      } catch {
        /* nothing more to do */
      }
    }
  },
  async open(path) {
    try {
      const tab = await api.OpenRepo(path);
      get().addTab(tab);
      await get().reloadState();
    } catch (e) {
      get().toast(e);
    }
  },
  async close(id) {
    try {
      await api.CloseTab(id);
    } catch (e) {
      get().toast(e);
      return;
    }
    set((s) => {
      const tabs = s.tabs.filter((t) => t.id !== id);
      const { [id]: _op, ...ops } = s.ops;
      return {
        tabs,
        ops,
        toasts: s.toasts.filter((t) => t.tab !== id),
        active: s.active === id ? (tabs.at(-1)?.id ?? null) : s.active,
      };
    });
    useRepo.setState((s) => {
      const { [id]: _repo, ...repos } = s.repos;
      return { repos };
    });
    await get().reloadState();
  },
  activate(id) {
    set({ active: id });
    api.SetActiveTab(id).catch((e) => get().toast(e));
  },
  addTab(tab) {
    set((s) => ({ tabs: s.tabs.some((t) => t.id === tab.id) ? s.tabs : [...s.tabs, tab], active: tab.id }));
  },
  async reloadState() {
    try {
      set({ state: await api.GetState() });
    } catch (e) {
      get().toast(e);
    }
  },
  toast(e, tab) {
    const error = parseError(e);
    if (isSilent(error)) return;
    set((s) => ({ toasts: [...s.toasts, { id: nextToast++, tab, error }] }));
  },
  dismiss(id) {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
  },
  setOp(tab, op) {
    set((s) => ({ ops: { ...s.ops, [tab]: op } }));
  },
  progressOp(tab, op) {
    if (tab !== '' && !get().tabs.some((t) => t.id === tab)) return;
    if (!get().ops[tab]) return;
    get().setOp(tab, op);
  },
  async runOp(tab, op, fn) {
    activeOps.set(tab, (activeOps.get(tab) ?? 0) + 1);
    if (activeOps.get(tab) === 1) get().setOp(tab, { op, phase: '', percent: 0 });
    try {
      return await fn();
    } finally {
      // No `return` here: it would swallow fn's result or error.
      const left = (activeOps.get(tab) ?? 1) - 1;
      if (left > 0) activeOps.set(tab, left);
      else {
        activeOps.delete(tab);
        set((s) => {
          const { [tab]: _done, ...ops } = s.ops;
          return { ops };
        });
      }
    }
  },
  pushAuth(p) {
    set((s) => ({ authQueue: [...s.authQueue, p] }));
  },
  shiftAuth() {
    set((s) => ({ authQueue: s.authQueue.slice(1) }));
  },
  // `auth:cancel`: the backend gave up on this prompt (timeout, cancelled op, shutdown).
  cancelAuth(id) {
    set((s) => ({ authQueue: s.authQueue.filter((p) => p.id !== id) }));
  },
}));
