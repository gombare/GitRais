import type { diff } from '../../wailsjs/go/models';

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
