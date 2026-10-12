import { describe, expect, it } from 'vitest';
import { graphWidth, laneColor, laneX, ROW_H, segLine, visibleRange, LANE_W, GRAPH_PAD } from './geometry';

describe('visibleRange', () => {
  it('computes visible rows with overscan', () => {
    expect(visibleRange(0, 10 * ROW_H, 1000, 5)).toEqual({ start: 0, end: 15 });
    expect(visibleRange(100 * ROW_H, 10 * ROW_H, 1000, 5)).toEqual({ start: 95, end: 115 });
  });
  it('clamps to the total', () => {
    expect(visibleRange(995 * ROW_H, 10 * ROW_H, 1000, 5)).toEqual({ start: 990, end: 1000 });
  });
  it('never returns start > end for an empty or shrunk list', () => {
    expect(visibleRange(500 * ROW_H, 10 * ROW_H, 0, 5)).toEqual({ start: 0, end: 0 });
  });
});

describe('segLine', () => {
  it('up goes from the top edge to the center', () => {
    expect(segLine([0, 1, 0], 'up', 56)).toEqual({ x1: laneX(0), y1: 56, x2: laneX(1), y2: 56 + ROW_H / 2 });
  });
  it('down goes from the center to the bottom edge', () => {
    expect(segLine([2, 2, 0], 'down', 0)).toEqual({ x1: laneX(2), y1: ROW_H / 2, x2: laneX(2), y2: ROW_H });
  });
});

describe('graphWidth', () => {
  it('uses the max lane among nodes and segments', () => {
    const w = graphWidth([{ lane: 0, up: [[0, 0, 0]], down: [[0, 3, 0]] }, undefined]);
    expect(w).toBe(GRAPH_PAD * 2 + 4 * LANE_W);
  });
});

describe('laneColor', () => {
  it('wraps by modulo, including the backend 65536 wrap', () => {
    expect(laneColor(0)).toBe(laneColor(12));
    expect(laneColor(65535)).toMatch(/^#/);
  });
});
