type Page<T> = { rows: T[]; stale: boolean };
type Job = { promise: Promise<void>; epoch: number };

// Paged row cache with LRU eviction. Pages are keyed by their first row index.
// - Memory is capped at maxPages pages (least recently read/loaded pages go first).
// - Concurrent ensure() calls share one in-flight fetch per page.
// - reset() invalidates everything, including fetches still in flight (generation check).
// - Partial pages (the tail of a graph still being built) can be flagged stale: they keep serving
//   their old rows until a refetch replaces them, so the UI does not flicker.
export class RowCache<T> {
  private pages = new Map<number, Page<T>>();
  private inflight = new Map<number, Job>();
  private generation = 0;
  private epoch = 0;

  constructor(
    private fetch: (start: number, count: number) => Promise<T[]>,
    private page = 500,
    private maxPages = 64,
  ) {}

  get size(): number {
    return this.pages.size;
  }

  get(i: number): T | undefined {
    const key = Math.floor(i / this.page) * this.page;
    const p = this.pages.get(key);
    if (!p) return undefined;
    // Refresh recency (Map keeps insertion order).
    this.pages.delete(key);
    this.pages.set(key, p);
    return p.rows[i - key];
  }

  // Loads the pages covering [start, end). Resolves true if it loaded (or awaited) pages that
  // changed the cache, false if nothing was needed or the cache was reset meanwhile.
  async ensure(start: number, end: number): Promise<boolean> {
    const gen = this.generation;
    const first = Math.floor(Math.max(0, start) / this.page) * this.page;
    const jobs: Promise<void>[] = [];
    for (let p = first; p < end; p += this.page) {
      const cached = this.pages.get(p);
      if (cached && !cached.stale) continue;
      let job = this.inflight.get(p);
      if (!job) {
        job = this.load(p, gen, first, end);
        this.inflight.set(p, job);
      }
      jobs.push(job.promise);
    }
    if (jobs.length === 0) return false;
    await Promise.all(jobs);
    return gen === this.generation;
  }

  // Flags every incomplete page for refetch (the backend graph grew).
  markPartialStale(): void {
    this.epoch++;
    for (const p of this.pages.values()) if (p.rows.length < this.page) p.stale = true;
  }

  reset(): void {
    this.generation++;
    this.pages.clear();
    this.inflight.clear();
  }

  private load(p: number, gen: number, keepFrom: number, keepTo: number): Job {
    const epoch = this.epoch;
    const promise = this.fetch(p, this.page).then(
      (rows) => {
        if (gen !== this.generation) return;
        this.inflight.delete(p);
        // The graph may have grown while this fetch ran: keep a short page stale.
        this.pages.delete(p);
        this.pages.set(p, { rows, stale: rows.length < this.page && epoch !== this.epoch });
        this.evict(keepFrom, keepTo);
      },
      (err) => {
        if (gen === this.generation) this.inflight.delete(p);
        throw err;
      },
    );
    return { promise, epoch };
  }

  private evict(keepFrom: number, keepTo: number): void {
    for (const key of this.pages.keys()) {
      if (this.pages.size <= this.maxPages) break;
      if (key + this.page > keepFrom && key < keepTo) continue; // never evict the range being loaded
      this.pages.delete(key);
    }
  }
}
