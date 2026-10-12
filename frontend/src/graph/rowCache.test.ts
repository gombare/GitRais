import { describe, expect, it, vi } from 'vitest';
import { RowCache } from './rowCache';

const range = async (start: number, count: number) => Array.from({ length: count }, (_, i) => start + i);

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

describe('RowCache', () => {
  it('loads by page and does not repeat requests', async () => {
    const fetch = vi.fn(range);
    const c = new RowCache(fetch, 100);
    expect(await c.ensure(150, 260)).toBe(true);
    expect(fetch).toHaveBeenCalledTimes(2); // pages 100-199 and 200-299
    expect(c.get(150)).toBe(150);
    expect(c.get(10)).toBeUndefined();
    expect(await c.ensure(160, 250)).toBe(false);
  });

  it('reset empties the cache', async () => {
    const c = new RowCache(range, 10);
    await c.ensure(0, 5);
    c.reset();
    expect(c.get(0)).toBeUndefined();
  });

  it('evicts least recently used pages beyond maxPages, keeping the requested range', async () => {
    const c = new RowCache(range, 10, 3);
    for (let p = 0; p < 10; p++) await c.ensure(p * 10, p * 10 + 10);
    expect(c.size).toBe(3);
    expect(c.get(0)).toBeUndefined();
    expect(c.get(95)).toBe(95);
    // touching a page keeps it alive
    await c.ensure(70, 80);
    c.get(70);
    await c.ensure(0, 10);
    expect(c.get(70)).toBe(70);
  });

  it('ignores responses that arrive after a reset', async () => {
    const d = deferred<number[]>();
    const c = new RowCache(() => d.promise, 10);
    const p = c.ensure(0, 10);
    c.reset();
    d.resolve([1, 2, 3]);
    expect(await p).toBe(false);
    expect(c.get(0)).toBeUndefined();
  });

  it('a stale in-flight job does not clobber the new generation', async () => {
    const old = deferred<number[]>();
    const fresh = deferred<number[]>();
    const fetch = vi.fn().mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise);
    const c = new RowCache<number>(fetch, 10);
    const a = c.ensure(0, 10);
    c.reset();
    const b = c.ensure(0, 10);
    expect(fetch).toHaveBeenCalledTimes(2);
    old.resolve([9]);
    await a;
    expect(c.get(0)).toBeUndefined();
    fresh.resolve([7]);
    await b;
    expect(c.get(0)).toBe(7);
  });

  it('dedupes concurrent in-flight fetches of the same page', async () => {
    const d = deferred<number[]>();
    const fetch = vi.fn(() => d.promise);
    const c = new RowCache<number>(fetch, 10);
    const a = c.ensure(0, 10);
    const b = c.ensure(2, 8);
    expect(fetch).toHaveBeenCalledTimes(1);
    d.resolve([1]);
    expect(await Promise.all([a, b])).toEqual([true, true]);
  });

  it('refetches a partial page after markPartialStale but keeps serving old data meanwhile', async () => {
    let n = 3;
    const fetch = vi.fn(async (s: number) => Array.from({ length: n }, (_, i) => s + i));
    const c = new RowCache(fetch, 10);
    await c.ensure(0, 10);
    expect(await c.ensure(0, 10)).toBe(false);
    n = 6;
    c.markPartialStale();
    expect(c.get(5)).toBeUndefined();
    expect(c.get(2)).toBe(2);
    expect(await c.ensure(0, 10)).toBe(true);
    expect(c.get(5)).toBe(5);
  });

  it('a failed fetch is not cached and can be retried', async () => {
    const fetch = vi.fn().mockRejectedValueOnce(new Error('x')).mockImplementation(range);
    const c = new RowCache<number>(fetch, 10);
    await expect(c.ensure(0, 10)).rejects.toThrow('x');
    c.markPartialStale(); // clears the failure backoff
    expect(await c.ensure(0, 10)).toBe(true);
    expect(c.get(3)).toBe(3);
  });

  it('refetches when markPartialStale happens while a fetch is in flight', async () => {
    let n = 0;
    const gates: Array<ReturnType<typeof deferred<number[]>>> = [];
    const fetch = vi.fn(() => { const d = deferred<number[]>(); gates[n++] = d; return d.promise; });
    const c = new RowCache<number>(fetch, 10);
    const a = c.ensure(0, 10);
    c.markPartialStale(); // backend grew after the first fetch started
    const b = c.ensure(0, 10);
    gates[0].resolve([0, 1, 2]); // old, short answer
    gates[1].resolve([0, 1, 2, 3, 4]); // newer answer
    await Promise.all([a, b]);
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(c.get(4)).toBe(4);
  });

  it('a superseded in-flight result never overwrites the newer one', async () => {
    const gates: Array<ReturnType<typeof deferred<number[]>>> = [];
    const fetch = vi.fn(() => { const d = deferred<number[]>(); gates.push(d); return d.promise; });
    const c = new RowCache<number>(fetch, 10);
    const a = c.ensure(0, 10);
    c.markPartialStale();
    const b = c.ensure(0, 10);
    gates[1].resolve([0, 1, 2, 3, 4]);
    await b;
    gates[0].resolve([0, 1, 2]);
    await a;
    expect(c.get(4)).toBe(4);
  });

  it('backs off a failed page until the delay passes or markPartialStale', async () => {
    vi.useFakeTimers();
    try {
      const fetch = vi.fn(async () => { throw new Error('boom'); });
      const c = new RowCache<number>(fetch, 10);
      await expect(c.ensure(0, 10)).rejects.toThrow('boom');
      expect(await c.ensure(0, 10)).toBe(false); // within backoff: no refetch, no throw
      expect(fetch).toHaveBeenCalledTimes(1);
      vi.advanceTimersByTime(5001);
      await expect(c.ensure(0, 10)).rejects.toThrow('boom');
      expect(fetch).toHaveBeenCalledTimes(2);
      c.markPartialStale();
      await expect(c.ensure(0, 10)).rejects.toThrow('boom');
      expect(fetch).toHaveBeenCalledTimes(3);
    } finally {
      vi.useRealTimers();
    }
  });
});
