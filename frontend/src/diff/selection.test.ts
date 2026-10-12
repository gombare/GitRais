import { describe, expect, it } from 'vitest';
import type { diff } from '../../wailsjs/go/models';
import { fingerprint, modelMatchesDiff, selectionMatchesModel, selectionToLines, splitLikeMonaco, type LineSel } from './selection';

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

// "a\rb\nc\nd\ne\n" -> "a\rb\nC\nD\ne\n": git sees 4 lines per side, Monaco 5 (a lone
// "\r" breaks a line in Monaco only).
const crOld = 'a\rb\nc\nd\ne\n';
const crNew = 'a\rb\nC\nD\ne\n';
const crFd = {
  status: 'M',
  hunks: [{ oldStart: 1, oldLines: 4, newStart: 1, newLines: 4, lines: [
    { kind: ' ', oldNo: 1, newNo: 1, text: 'a\rb' },
    { kind: '-', oldNo: 2, newNo: 0, text: 'c' }, { kind: '-', oldNo: 3, newNo: 0, text: 'd' },
    { kind: '+', oldNo: 0, newNo: 2, text: 'C' }, { kind: '+', oldNo: 0, newNo: 3, text: 'D' },
    { kind: ' ', oldNo: 4, newNo: 4, text: 'e' } ] }],
} as unknown as diff.FileDiff;

const crlfFd = {
  status: 'M',
  hunks: [{ oldStart: 1, oldLines: 2, newStart: 1, newLines: 2, lines: [
    { kind: ' ', oldNo: 1, newNo: 1, text: 'x\r' },
    { kind: '-', oldNo: 2, newNo: 0, text: 'y\r' }, { kind: '+', oldNo: 0, newNo: 2, text: 'Y\r' },
    { kind: '\\', oldNo: 0, newNo: 0, text: ' No newline at end of file' } ] }],
} as unknown as diff.FileDiff;

describe('splitLikeMonaco', () => {
  it('breaks lines at \\r\\n, \\n and a lone \\r', () => {
    const m = splitLikeMonaco('a\r\nb\rc\nd');
    expect(m.getLineCount()).toBe(4);
    expect([1, 2, 3, 4].map((n) => m.getLineContent(n))).toEqual(['a', 'b', 'c', 'd']);
    expect(m.getLineContent(9)).toBe('');
  });
});

describe('modelMatchesDiff', () => {
  it('accepts a diff whose lines are at the same numbers in the model (CRLF stripped)', () => {
    expect(modelMatchesDiff(crlfFd, splitLikeMonaco('x\r\ny\r\n'), splitLikeMonaco('x\r\nY\r\n'))).toBe(true);
  });
  it('rejects a model whose lines are split differently than git splits them', () => {
    expect(modelMatchesDiff(crFd, splitLikeMonaco(crOld), splitLikeMonaco(crNew))).toBe(false);
  });
  it('rejects a model with different content (e.g. a clean filter)', () => {
    expect(modelMatchesDiff(crlfFd, splitLikeMonaco('x\r\ny\r\n'), splitLikeMonaco('x\r\nZ\r\n'))).toBe(false);
  });
});

describe('selectionMatchesModel', () => {
  it('refuses when a selected Monaco line is not the matched hunk line', () => {
    // Monaco modified line 3 is "C"; git's new line 3 is "D".
    const sel = selectionToLines(crFd, new Set(), new Set([3]));
    expect(sel).toEqual({ hunk: 0, lines: [4] });
    expect(selectionMatchesModel(crFd, sel as LineSel, splitLikeMonaco(crOld), splitLikeMonaco(crNew))).toBe(false);
  });
  it('accepts a selection whose lines match', () => {
    expect(selectionMatchesModel(crlfFd, { hunk: 0, lines: [1, 2] }, splitLikeMonaco('x\r\ny\r\n'), splitLikeMonaco('x\r\nY\r\n'))).toBe(true);
  });
});

describe('fingerprint', () => {
  it('holds the hunk header and the selected lines in order', () => {
    expect(fingerprint(crFd, 0, [1, 4])).toEqual({
      oldStart: 1, oldLines: 4, newStart: 1, newLines: 4,
      lines: [{ kind: '-', text: 'c' }, { kind: '+', text: 'D' }],
    });
  });
  it('holds every hunk line for a whole hunk', () => {
    expect(fingerprint(crFd, 0, []).lines).toHaveLength(6);
  });
});
