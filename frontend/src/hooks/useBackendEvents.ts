import { useEffect } from 'react';
import { on, type AuthPrompt } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';

type TabEvent = { tab: string };
type GraphProgress = { tab: string; rows: number; done: boolean; error?: string };
// gitexec.Progress is embedded; its JSON tags are promoted (phase, percent, current, total).
type OpProgress = { tab: string; op: string; phase: string; percent: number; current: number; total: number };
type AuthCancel = { id: string };
type AutoFetchEvent = { tab: string; enabled: boolean; paused: boolean; reason: string; lastFetch: number };

export function useBackendEvents() {
  useEffect(() => {
    const offs = [
      on<TabEvent>('wip:changed', (e) => useRepo.getState().refreshStatus(e.tab)),
      on<TabEvent>('refs:changed', (e) => useRepo.getState().refreshRefs(e.tab)),
      on<TabEvent>('graph:reset', (e) => useRepo.getState().graphReset(e.tab)),
      on<GraphProgress>('graph:progress', (e) => {
        useRepo.getState().graphProgress(e.tab, e.rows, e.done);
        if (e.error) useApp.getState().toast(e.error, e.tab);
      }),
      on<OpProgress>('op:progress', (e) =>
        useApp.getState().progressOp(e.tab, { op: e.op, phase: e.phase ?? '', percent: e.percent ?? 0 })),
      on<AutoFetchEvent>('autofetch:status', ({ tab, ...a }) => useRepo.getState().setAutoFetch(tab, a)),
      on<AuthPrompt>('auth:prompt', (p) => useApp.getState().pushAuth(p)),
      on<AuthCancel>('auth:cancel', (c) => useApp.getState().cancelAuth(c.id)),
    ];
    return () => offs.forEach((off) => off());
  }, []);
}
