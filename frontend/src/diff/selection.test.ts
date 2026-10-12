import { describe, expect, it } from 'vitest';
import type { diff } from '../../wailsjs/go/models';
import { selectionToLines } from './selection';

const fd = {
  hunks: [
    { lines: [
      { kind: ' ', oldNo: 1, newNo: 1 }, { kind: '-', oldNo: 2, newNo: 0 }, { kind: '+', oldNo: 0, newNo: 2 },
      { kind: ' ', oldNo: 3, newNo: 3 } ] },
    { lines: [
      { kind: ' ', oldNo: 10, newNo: 10 }, { kind: '+', oldNo: 0, newNo: 11 },
      { kind: '-', oldNo: 11, newNo: 0 }, { kind: '-', oldNo: 12, newNo: 0 }, { kind: '+', oldNo: 0, newNo: 12 },
      { kind: ' ', oldNo: 13, newNo: 13 } ] },
  ],
} as unknown as diff.FileDiff;

describe('selectionToLines', () => {
  it('maps original and modified lines in the same hunk', () => {
    expect(selectionToLines(fd, new Set([2]), new Set([2]))).toEqual({ hunk: 0, lines: [1, 2] });
  });
  it('ignores context lines', () => {
    expect(selectionToLines(fd, new Set(), new Set([1, 3]))).toBeNull();
    expect(selectionToLines(fd, new Set([1, 3, 10]), new Set())).toBeNull();
  });
  it('returns null for an empty selection', () => {
    expect(selectionToLines(fd, new Set(), new Set())).toBeNull();
  });
  it('rejects selections across hunks', () => {
    expect(selectionToLines(fd, new Set(), new Set([2, 11]))).toHaveProperty('error');
    expect(selectionToLines(fd, new Set([2]), new Set([11]))).toHaveProperty('error');
  });
  it('maps deletions on the original side only by old line number', () => {
    // Old line 11 is a deletion; new line 11 is an addition in the same hunk.
    expect(selectionToLines(fd, new Set([11, 12]), new Set())).toEqual({ hunk: 1, lines: [2, 3] });
  });
  it('maps additions on the modified side only by new line number', () => {
    expect(selectionToLines(fd, new Set(), new Set([11, 12]))).toEqual({ hunk: 1, lines: [1, 4] });
  });
  it('maps a mixed selection with context in between, in hunk order', () => {
    expect(selectionToLines(fd, new Set([12, 13]), new Set([10, 11]))).toEqual({ hunk: 1, lines: [1, 3] });
  });
  it('ignores hunks without lines and files without hunks', () => {
    expect(selectionToLines({ hunks: [] } as unknown as diff.FileDiff, new Set([1]), new Set([1]))).toBeNull();
  });
});
