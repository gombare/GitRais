import type { diff, session } from '../../wailsjs/go/models';

// A StageLines request body: indexes into fd.hunks[hunk].lines.
export type LineSel = { hunk: number; lines: number[] };

// Maps the lines selected in a diff editor to the changed lines of one hunk.
// origLines/modLines are 1-based line numbers selected in the original and
// modified editors: deletions match on the original side, additions on the
// modified side, context lines are ignored. Returns null when no changed line
// is selected and an error when the selection spans more than one hunk.
export function selectionToLines(
  fd: diff.FileDiff, origLines: Set<number>, modLines: Set<number>,
): LineSel | { error: string } | null {
  const picked = new Map<number, number[]>();
  (fd.hunks ?? []).forEach((h, hi) => (h.lines ?? []).forEach((l, li) => {
    const hit = (l.kind === '+' && modLines.has(l.newNo)) || (l.kind === '-' && origLines.has(l.oldNo));
    if (!hit) return;
    const lines = picked.get(hi);
    if (lines) lines.push(li);
    else picked.set(hi, [li]);
  }));
  if (picked.size === 0) return null;
  if (picked.size > 1) return { error: 'Seleziona righe di un solo blocco di modifiche.' };
  const [[hunk, lines]] = [...picked.entries()];
  return { hunk, lines };
}

// The part of a Monaco text model the checks below read (1-based lines).
export type LineSource = { getLineCount(): number; getLineContent(n: number): string };

// Splits text into lines the way Monaco does: "\r\n", "\n" and a lone "\r" all
// end a line (git only breaks at "\n").
export function splitLikeMonaco(text: string): LineSource {
  const lines = text.split(/\r\n|\r|\n/);
  return { getLineCount: () => lines.length, getLineContent: (n) => lines[n - 1] ?? '' };
}

// A diff line's text as Monaco shows it: without the "\r" of a CRLF ending.
const shown = (text: string) => (text.endsWith('\r') ? text.slice(0, -1) : text);

function lineMatches(l: diff.Line, orig: LineSource, mod: LineSource): boolean {
  const at = (m: LineSource, n: number) => n >= 1 && n <= m.getLineCount() && m.getLineContent(n) === shown(l.text);
  switch (l.kind) {
    case '-': return at(orig, l.oldNo);
    case '+': return at(mod, l.newNo);
    case ' ': return at(orig, l.oldNo) && at(mod, l.newNo);
    default: return true; // "\ No newline at end of file" has no model line
  }
}

// Reports whether every hunk line sits at its line number in the models. When it
// does not (a lone "\r", a clean filter such as LFS), Monaco line numbers cannot
// be mapped to hunk lines and line selection staging must be off.
export function modelMatchesDiff(fd: diff.FileDiff, orig: LineSource, mod: LineSource): boolean {
  return (fd.hunks ?? []).every((h) => (h.lines ?? []).every((l) => lineMatches(l, orig, mod)));
}

// Reports whether each selected hunk line is the text the models show at its number.
export function selectionMatchesModel(fd: diff.FileDiff, sel: LineSel, orig: LineSource, mod: LineSource): boolean {
  const lines = fd.hunks?.[sel.hunk]?.lines ?? [];
  return sel.lines.every((i) => !!lines[i] && lineMatches(lines[i], orig, mod));
}

// What the backend must still find before staging lines (all of them when
// lines is empty) of fd.hunks[hunk]: the header and each line's kind and text.
export function fingerprint(fd: diff.FileDiff, hunk: number, lines: number[]): session.HunkFingerprint {
  const h = fd.hunks[hunk];
  const all = h.lines ?? [];
  const picked = lines.length > 0 ? lines.map((i) => all[i]) : all;
  return {
    oldStart: h.oldStart, oldLines: h.oldLines, newStart: h.newStart, newLines: h.newLines,
    lines: picked.map((l) => ({ kind: l.kind, text: l.text })),
  } as session.HunkFingerprint;
}
