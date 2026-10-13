import { useCallback, useEffect, useId, useImperativeHandle, useMemo, useRef, useState, type Ref } from 'react';
import { api } from '../api/client';
import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { RowCache } from '../graph/rowCache';
import { useDevicePixelRatio } from '../hooks/useDevicePixelRatio';
import { drawGraph } from '../graph/draw';
import { openMenu, openMenuAt, type MenuItem } from './ContextMenu';
import { commitMenu, stashMenu } from '../actions/repoActions';
import { graphWidth, GRAPH_PAD, LANE_W, ROW_H, visibleRange } from '../graph/geometry';
import { fullDate, relativeDate } from '../format/date';
import { useNow } from '../hooks/useNow';
import type { app, gitread } from '../../wailsjs/go/models';

const PAGE = 500;
const MAX_PAGES = 64; // ~32k rows resident, however far the user scrolls
const MAX_GRAPH_W = GRAPH_PAD * 2 + 40 * LANE_W; // very wide graphs are clipped, not allowed to eat the row text

// Imperative API for sibling features (selection, search, "go to HEAD"...).
export type GraphHandle = { scrollToRow(index: number): void };

function refsByHash(refs?: gitread.Refs): Map<string, gitread.Ref[]> {
  const m = new Map<string, gitread.Ref[]>();
  for (const r of refs?.refs ?? []) m.set(r.hash, [...(m.get(r.hash) ?? []), r]);
  return m;
}

// Stash rows get the same actions as the left panel's stash list; commits get checkout/branch/tag/copy.
const rowMenu = (tab: string, r: app.RowDTO): MenuItem[] => r.kind === 'stash'
  ? stashMenu(tab, { Hash: r.hash, Ref: r.stashRef, Subject: r.subject })
  : commitMenu(tab, r.hash);

export function GraphView({ tab, handleRef }: { tab: string; handleRef?: Ref<GraphHandle> }) {
  const total = useRepo((s) => s.repos[tab]?.graphRows ?? 0);
  const version = useRepo((s) => s.repos[tab]?.graphVersion ?? 0);
  const done = useRepo((s) => s.repos[tab]?.graphDone ?? false);
  const selection = useRepo((s) => s.repos[tab]?.selection);
  const refs = useRepo((s) => s.repos[tab]?.refs);
  const wipCount = useRepo((s) => s.repos[tab]?.status?.files.length ?? 0);
  const select = useRepo((s) => s.select);
  const selected = selection?.kind === 'commit' ? selection.hash : null;
  const labels = useMemo(() => refsByHash(refs), [refs]);
  const now = useNow();

  const scroller = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const scrollTopRef = useRef(0);
  const selIndex = useRef(-1);
  const [viewport, setViewport] = useState(600);
  const [tick, force] = useState(0);
  const [width, setWidth] = useState(80);
  const dpr = useDevicePixelRatio();
  const listId = useId();
  const warned = useRef<RowCache<app.RowDTO> | null>(null); // cache that already showed its error toast

  // A new cache per (tab, graph version): graph:reset invalidates every row and any in-flight page.
  const cache = useMemo(() => {
    void version;
    return new RowCache<app.RowDTO>((s, n) => api.Rows(tab, s, n), PAGE, MAX_PAGES);
  }, [tab, version]);
  useEffect(() => () => cache.reset(), [cache]);

  // The DOM window only changes when the first/last visible row changes, not on every scroll frame.
  const [range, setRange] = useState(() => visibleRange(0, 600, total));
  const updateRange = useCallback(() => {
    const r = visibleRange(scrollTopRef.current, viewport, total);
    setRange((prev) => (prev.start === r.start && prev.end === r.end ? prev : r));
  }, [viewport, total]);
  useEffect(updateRange, [updateRange]);
  const { start, end } = range;

  // Canvas drawing is coalesced into one requestAnimationFrame and reads the cache directly.
  const raf = useRef(0);
  const draw = useCallback(() => {
    const c = canvas.current;
    const ctx = c?.getContext('2d');
    if (!c || !ctx) return;
    const vr = visibleRange(scrollTopRef.current, viewport, total, 2);
    const rows: Array<app.RowDTO | undefined> = [];
    for (let i = vr.start; i < vr.end; i++) rows.push(cache.get(i));
    drawGraph(ctx, rows, vr.start, scrollTopRef.current, width, viewport, selected, dpr);
  }, [cache, viewport, total, width, selected, dpr]);
  const drawRef = useRef(draw);
  drawRef.current = draw;
  const schedule = useCallback(() => {
    if (raf.current) return;
    raf.current = requestAnimationFrame(() => {
      raf.current = 0;
      drawRef.current();
    });
  }, []);
  useEffect(() => () => {
    cancelAnimationFrame(raf.current);
    raf.current = 0; // StrictMode re-runs effects: a stale non-zero id would block scheduling forever
  }, []);

  const rows = useMemo(() => {
    void tick; // recompute when the cache loaded new pages
    return Array.from({ length: end - start }, (_, k) => cache.get(start + k));
  }, [cache, start, end, tick]);

  const activeIdx = useMemo(() => {
    const k = selected ? rows.findIndex((r) => r?.hash === selected) : -1;
    return k >= 0 ? start + k : -1;
  }, [rows, selected, start]);

  // The graph may still be growing: its incomplete tail page must be refetched.
  useEffect(() => {
    cache.markPartialStale();
  }, [cache, total, done]);

  useEffect(() => {
    let live = true;
    cache.ensure(start, end).then(
      (loaded) => {
        if (loaded && warned.current === cache) warned.current = null; // a page really loaded again
        if (live && loaded) force((x) => x + 1);
      },
      (e) => {
        // One toast per cache instance until a fetch succeeds: a persistent failure must not spam.
        if (live && warned.current !== cache) {
          warned.current = cache;
          useApp.getState().toast(e, tab);
        }
      },
    );
    return () => {
      live = false;
    };
  }, [cache, start, end, total, done, tab]);

  // The width only grows while the same cache is active (no jitter as rows scroll in and out).
  useEffect(() => setWidth(80), [cache]);
  useEffect(() => {
    const w = Math.min(MAX_GRAPH_W, Math.max(80, graphWidth(rows)));
    setWidth((prev) => Math.max(prev, w));
  }, [rows]);

  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    setViewport(el.clientHeight);
    const ro = new ResizeObserver(() => setViewport(el.clientHeight));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // HiDPI backing store; resizing it clears the bitmap, so only do it when the size changes.
  useEffect(() => {
    const c = canvas.current;
    if (!c) return;
    c.width = Math.round(width * dpr);
    c.height = Math.round(viewport * dpr);
    schedule();
  }, [width, viewport, dpr, schedule]);

  useEffect(schedule, [draw, tick, schedule]);

  const ensureVisible = useCallback((i: number) => {
    const el = scroller.current;
    if (!el) return;
    if (i * ROW_H < el.scrollTop) el.scrollTop = i * ROW_H;
    else if ((i + 1) * ROW_H > el.scrollTop + el.clientHeight) el.scrollTop = (i + 1) * ROW_H - el.clientHeight;
  }, []);

  useImperativeHandle(handleRef, () => ({
    scrollToRow(i: number) {
      const el = scroller.current;
      if (el) el.scrollTop = Math.max(0, Math.min(total - 1, i)) * ROW_H - el.clientHeight / 2 + ROW_H / 2;
    },
  }), [total]);

  const selectRow = useCallback((i: number, r: app.RowDTO) => {
    selIndex.current = i;
    select(tab, { kind: 'commit', hash: r.hash });
  }, [select, tab]);

  const onKey = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) {
      e.preventDefault();
      if (!selected) return;
      const k = rows.findIndex((r) => r?.hash === selected);
      const r = k >= 0 ? rows[k] : cache.get(selIndex.current)?.hash === selected ? cache.get(selIndex.current) : undefined;
      if (!r) return;
      // Anchor to the selected row when it is rendered, otherwise to the list itself.
      const el = k >= 0 ? document.getElementById(`${listId}-opt-${start + k}`) : null;
      openMenuAt(el ?? e.currentTarget as HTMLElement, rowMenu(tab, r));
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    let cur = -1;
    if (selected) {
      if (cache.get(selIndex.current)?.hash === selected) cur = selIndex.current;
      else {
        const k = rows.findIndex((r) => r?.hash === selected);
        if (k >= 0) cur = start + k;
      }
    }
    const next = Math.max(0, Math.min(total - 1, cur + (e.key === 'ArrowDown' ? 1 : -1)));
    if (total === 0) return;
    ensureVisible(next);
    const r = cache.get(next);
    if (r) selectRow(next, r);
    else cache.ensure(next, next + 1).then(() => {
      const loaded = cache.get(next);
      if (loaded) selectRow(next, loaded);
    }, () => undefined);
  }, [selected, rows, start, total, cache, selectRow, ensureVisible, listId, tab]);

  return (
    <div className="graph">
      {wipCount > 0 && (
        <div className={`graph-wip ${selection?.kind === 'wip' ? 'selected' : ''}`}
          onClick={() => select(tab, { kind: 'wip' })}>
          <span className="wip-dot" /> WIP <span className="muted">{wipCount} file modificati</span>
        </div>
      )}
      <div className="graph-scroll" ref={scroller} tabIndex={0} onKeyDown={onKey}
        role="listbox" aria-label="Storia dei commit" aria-keyshortcuts="Shift+F10 ContextMenu"
        aria-activedescendant={activeIdx >= 0 ? `${listId}-opt-${activeIdx}` : undefined}
        onScroll={(e) => {
          scrollTopRef.current = e.currentTarget.scrollTop;
          updateRange();
          schedule();
        }}>
        <div style={{ height: total * ROW_H, position: 'relative' }}>
          <canvas ref={canvas} className="graph-canvas" aria-hidden="true"
            style={{ width, height: viewport, marginBottom: -viewport }} />
          {rows.map((r, k) => {
            const i = start + k;
            return (
              <div key={i} id={`${listId}-opt-${i}`} role="option" aria-selected={!!r && r.hash === selected}
                aria-posinset={i + 1} aria-setsize={total}
                className={`graph-row ${r && r.hash === selected ? 'selected' : ''}`}
                style={{ top: i * ROW_H, height: ROW_H, paddingLeft: width }}
                onClick={() => r && selectRow(i, r)}
                onContextMenu={(e) => {
                  e.preventDefault(); // no native WebView menu, even on a row still loading
                  if (!r) return;
                  selectRow(i, r);
                  // Keep keyboard focus on the list so Escape returns there.
                  scroller.current?.focus();
                  openMenu(e, rowMenu(tab, r));
                }}>
                {r ? (
                  <>
                    <span className="graph-refs">
                      {(labels.get(r.hash) ?? []).map((ref) => (
                        <span key={ref.name} className={`ref ref-${ref.kind} ${ref.isHead ? 'head' : ''}`}>{ref.short}</span>
                      ))}
                      {r.kind === 'stash' && <span className="ref ref-stash">{r.stashRef}</span>}
                    </span>
                    <span className="graph-subject">{r.subject}</span>
                    <span className="graph-author muted">{r.author}</span>
                    <span className="graph-date muted" title={fullDate(r.time)}>{relativeDate(r.time, now)}</span>
                  </>
                ) : <span className="muted">…</span>}
              </div>
            );
          })}
        </div>
      </div>
      {!done && <div className="graph-status muted">Caricamento storia… {total.toLocaleString()} commit</div>}
    </div>
  );
}
