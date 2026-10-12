import { create } from 'zustand';
import { api, parseError, type AuthPrompt, type UIError } from '../api/client';
import { isSilent } from '../api/errors';
import type { app, store } from '../../wailsjs/go/models';

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
  pushAuth(p: AuthPrompt): void;
  shiftAuth(): void;
  cancelAuth(id: string): void;
};

let nextToast = 1;

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
      const tabs = (await api.RestoreTabs()) ?? [];
      const state = await api.GetState();
      const active = tabs.find((t) => t.id === state.activeTab)?.id ?? tabs[0]?.id ?? null;
      set({ tabs, state, active });
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
      return { tabs, active: s.active === id ? (tabs.at(-1)?.id ?? null) : s.active };
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
