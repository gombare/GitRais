import { describe, expect, it } from 'vitest';
import { groupRefs, remoteBranchName } from './groupRefs';

const r = (name: string, short: string, kind: string, remote = '') => ({ name, short, kind, remote } as any);

describe('groupRefs', () => {
  const refs = [
    r('refs/heads/main', 'main', 'local'), r('refs/heads/feat/x', 'feat/x', 'local'),
    r('refs/remotes/origin/main', 'origin/main', 'remote', 'origin'),
    r('refs/remotes/up/dev', 'up/dev', 'remote', 'up'), r('refs/tags/v1', 'v1', 'tag'),
  ];
  it('groups by kind and remote', () => {
    const g = groupRefs(refs, '');
    expect(g.local.map((x) => x.short)).toEqual(['feat/x', 'main']);
    expect(Object.keys(g.remotes)).toEqual(['origin', 'up']);
    expect(g.tags.length).toBe(1);
  });
  it('filters case-insensitively', () => {
    const g = groupRefs(refs, 'MAIN');
    expect(g.local.map((x) => x.short)).toEqual(['main']);
    expect(g.remotes.origin.length).toBe(1);
    expect(g.remotes.up).toBeUndefined();
    expect(g.tags.length).toBe(0);
  });
  it('keeps branches whose name contains "/" under their remote', () => {
    const g = groupRefs([r('refs/remotes/origin/feat/a/b', 'origin/feat/a/b', 'remote', 'origin')], '');
    expect(Object.keys(g.remotes)).toEqual(['origin']);
    expect(remoteBranchName(g.remotes.origin[0])).toBe('feat/a/b');
  });
  it('excludes symbolic remote HEAD refs', () => {
    const g = groupRefs([
      r('refs/remotes/origin/HEAD', 'origin/HEAD', 'remote', 'origin'),
      r('refs/remotes/origin/main', 'origin/main', 'remote', 'origin'),
    ], '');
    expect(g.remotes.origin.map((x) => x.short)).toEqual(['origin/main']);
    expect(Object.keys(groupRefs([r('refs/remotes/o/HEAD', 'o/HEAD', 'remote', 'o')], '').remotes)).toEqual([]);
  });
  it('handles a detached HEAD (no local ref flagged) and empty input', () => {
    const g = groupRefs([r('refs/heads/main', 'main', 'local')], '');
    expect(g.local.map((x) => x.short)).toEqual(['main']);
    expect(groupRefs([], 'x')).toEqual({ local: [], remotes: {}, tags: [] });
  });
});
