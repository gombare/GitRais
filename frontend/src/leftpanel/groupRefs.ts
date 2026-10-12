import type { gitread } from '../../wailsjs/go/models';

export type GroupedRefs = {
  local: gitread.Ref[];
  remotes: Record<string, gitread.Ref[]>;
  tags: gitread.Ref[];
};

// Symbolic remote HEADs (refs/remotes/<remote>/HEAD) are aliases, not branches.
const isSymbolicHead = (r: gitread.Ref) => r.kind === 'remote' && (r.short === 'HEAD' || r.short.endsWith('/HEAD'));

// Groups refs by kind (local / per-remote / tag), sorted by short name, keeping those that match
// `filter` (case-insensitive substring of the short name). A detached HEAD has no ref entry.
export function groupRefs(refs: gitread.Ref[], filter: string): GroupedRefs {
  const f = filter.trim().toLowerCase();
  const keep = (r: gitread.Ref) => !isSymbolicHead(r) && (!f || r.short.toLowerCase().includes(f));
  const byShort = (a: gitread.Ref, b: gitread.Ref) => a.short.localeCompare(b.short);
  const local = refs.filter((r) => r.kind === 'local' && keep(r)).sort(byShort);
  const tags = refs.filter((r) => r.kind === 'tag' && keep(r)).sort(byShort);
  const remotes: Record<string, gitread.Ref[]> = {};
  for (const r of refs.filter((x) => x.kind === 'remote' && keep(x)).sort(byShort)) {
    (remotes[r.remote] ??= []).push(r);
  }
  return { local, remotes, tags };
}

// Branch name on the remote: "origin/feat/x" -> "feat/x" (the branch itself may contain "/").
export function remoteBranchName(r: gitread.Ref): string {
  return r.short.startsWith(`${r.remote}/`) ? r.short.slice(r.remote.length + 1) : r.short;
}
