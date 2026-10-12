export const ROW_H = 28;
export const LANE_W = 14;
export const GRAPH_PAD = 10;
export const NODE_R = 5;

const PALETTE = ['#4fa3ff', '#4cd08a', '#f5a524', '#e05ee0', '#36c5d1', '#ff6b6b',
  '#a98bff', '#c4d14a', '#ff8f5a', '#5ad1a0', '#d17f9b', '#8fb3ff'];

// The backend assigns ever-growing color ids (wrapping at 65536): map them onto the palette.
export function laneColor(i: number): string {
  return PALETTE[((i % PALETTE.length) + PALETTE.length) % PALETTE.length];
}

export function visibleRange(scrollTop: number, viewport: number, total: number, overscan = 10) {
  const first = Math.floor(scrollTop / ROW_H);
  const count = Math.ceil(viewport / ROW_H);
  const end = Math.min(total, first + count + overscan);
  return { start: Math.min(end, Math.max(0, first - overscan)), end };
}

export function laneX(lane: number): number {
  return GRAPH_PAD + lane * LANE_W + LANE_W / 2;
}

export function segLine(seg: [number, number, number], half: 'up' | 'down', rowTop: number) {
  const mid = rowTop + ROW_H / 2;
  return half === 'up'
    ? { x1: laneX(seg[0]), y1: rowTop, x2: laneX(seg[1]), y2: mid }
    : { x1: laneX(seg[0]), y1: mid, x2: laneX(seg[1]), y2: rowTop + ROW_H };
}

export function graphWidth(rows: Array<{ lane: number; up: number[][]; down: number[][] } | undefined>): number {
  let max = 0;
  for (const r of rows) {
    if (!r) continue;
    max = Math.max(max, r.lane);
    for (const s of r.up) max = Math.max(max, s[0], s[1]);
    for (const s of r.down) max = Math.max(max, s[0], s[1]);
  }
  return GRAPH_PAD * 2 + (max + 1) * LANE_W;
}
