import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { CommitDetails } from './CommitDetails';
import { WipPanel } from './WipPanel';

// Right panel: the selected commit's details, or the working tree (WIP) with the commit form.
// Props that later tasks rely on: a file click calls useRepo.openDiff(tab, DiffTarget), and the diff
// viewer reads repos[tab].diff.
export function DetailPanel({ tab }: { tab: string }) {
  const sel = useRepo((s) => s.repos[tab]?.selection) ?? { kind: 'wip' as const };
  const path = useApp((s) => s.tabs.find((t) => t.id === tab)?.path ?? '');
  // key: never share form state between tabs or between different commits.
  return sel.kind === 'commit'
    ? <CommitDetails key={`${tab}:${sel.hash}`} tab={tab} hash={sel.hash} />
    : <WipPanel key={tab} tab={tab} path={path} />;
}
