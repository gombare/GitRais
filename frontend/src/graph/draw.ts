import type { app } from '../../wailsjs/go/models';
import { laneColor, laneX, NODE_R, ROW_H, segLine } from './geometry';

// Draws the rows [start, start + rows.length) into a canvas whose CSS size is width x height.
// Only the rows passed in are touched: the caller restricts them to the visible window.
export function drawGraph(
  ctx: CanvasRenderingContext2D, rows: Array<app.RowDTO | undefined>, start: number,
  scrollTop: number, width: number, height: number, selected: string | null,
) {
  const dpr = window.devicePixelRatio || 1;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, width, height);
  ctx.lineWidth = 2;
  rows.forEach((r, k) => {
    if (!r) return;
    const top = (start + k) * ROW_H - scrollTop;
    if (top > height || top + ROW_H < 0) return;
    for (const [half, list] of [['up', r.up], ['down', r.down]] as const) {
      for (const s of list as [number, number, number][]) {
        const l = segLine(s, half, top);
        ctx.strokeStyle = laneColor(s[2]);
        ctx.beginPath();
        ctx.moveTo(l.x1, l.y1);
        if (l.x1 === l.x2) ctx.lineTo(l.x2, l.y2);
        else ctx.bezierCurveTo(l.x1, (l.y1 + l.y2) / 2, l.x2, (l.y1 + l.y2) / 2, l.x2, l.y2);
        ctx.stroke();
      }
    }
    const cx = laneX(r.lane);
    const cy = top + ROW_H / 2;
    ctx.beginPath();
    ctx.arc(cx, cy, r.kind === 'stash' ? NODE_R - 1 : NODE_R, 0, Math.PI * 2);
    ctx.fillStyle = r.kind === 'stash' ? '#16181d' : laneColor(r.color);
    ctx.strokeStyle = laneColor(r.color);
    ctx.fill();
    ctx.stroke();
    if (r.hash === selected) {
      ctx.beginPath();
      ctx.arc(cx, cy, NODE_R + 3, 0, Math.PI * 2);
      ctx.strokeStyle = '#ffffff';
      ctx.stroke();
    }
  });
}
