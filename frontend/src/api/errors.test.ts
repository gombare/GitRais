import { describe, expect, it } from 'vitest';
import { parseError } from './client';
import { describeError, isSilent } from './errors';

describe('parseError', () => {
  it('reads the UIError JSON', () => {
    const e = parseError('{"kind":"AuthFailed","message":"m","stderr":"s"}');
    expect(e).toEqual({ kind: 'AuthFailed', message: 'm', stderr: 's' });
  });
  it('degrades on plain text', () => {
    expect(parseError(new Error('boom')).kind).toBe('Unknown');
  });
  it('parses the JSON carried by an Error', () => {
    expect(parseError(new Error('{"kind":"Cancelled","message":"","stderr":""}')).kind).toBe('Cancelled');
  });
});

describe('describeError', () => {
  it('proposes pull for NonFastForward', () => {
    expect(describeError({ kind: 'NonFastForward', message: '', stderr: '' }).action).toBe('pull');
  });
  it('proposes lock removal for Locked', () => {
    expect(describeError({ kind: 'Locked', message: '', stderr: '' }).action).toBe('removeLock');
  });
  it('shows the hook output', () => {
    const d = describeError({ kind: 'HookRejected', message: 'x', stderr: 'lint failed' });
    expect(d.stderr).toBe('lint failed');
    expect(d.detail).not.toContain('lint failed');
  });
  it('classifies notices by severity; unknown kinds are errors', () => {
    const sev = (kind: string) => describeError({ kind, message: '', stderr: '' }).severity;
    expect(sev('LockRemoved')).toBe('success');
    expect(sev('NoLinesSelected')).toBe('info');
    expect(sev('OpInProgress')).toBe('info');
    expect(sev('LockInUse')).toBe('warning');
    expect(sev('AuthFailed')).toBe('error');
    expect(sev('Whatever')).toBe('error');
  });
  it('describes backend sentinel kinds', () => {
    for (const kind of ['GitMissing', 'Timeout', 'InvalidPath', 'NoTab', 'PullStrategyRequired', 'OpInProgress', 'GitRunning']) {
      expect(describeError({ kind, message: 'raw', stderr: '' }).title).not.toBe('Errore');
    }
  });
  it('falls back to the raw message for unknown kinds', () => {
    expect(describeError({ kind: 'Whatever', message: 'raw', stderr: '' }).detail).toBe('raw');
  });
});

describe('isSilent', () => {
  it('treats Cancelled as silent only', () => {
    expect(isSilent({ kind: 'Cancelled', message: '', stderr: '' })).toBe(true);
    expect(isSilent({ kind: 'Timeout', message: '', stderr: '' })).toBe(false);
  });
});

// Every kind the backend can return has an Italian text (Cancelled is never shown; Unknown
// carries the raw message). The kinds are read from the Go sources, so a new one fails here.
import { readFileSync } from 'node:fs';
describe('backend kinds', () => {
  const go = (p: string) => readFileSync(new URL(`../../../internal/${p}`, import.meta.url), 'utf8');
  const kinds = new Set<string>();
  for (const m of go('app/errors.go').matchAll(/\{[\w.]+, "(\w+)"\}/g)) kinds.add(m[1]);
  for (const m of go('gitexec/errors.go').matchAll(/Kind\w*\s+Kind = "(\w+)"/g)) kinds.add(m[1]);
  it('finds the kinds in the Go sources', () => {
    expect(kinds.has('SwitchedNotUpdated')).toBe(true);
    expect(kinds.has('UntrackedWouldBeOverwritten')).toBe(true);
    expect(kinds.size).toBeGreaterThan(30);
  });
  it.each([...kinds].filter((k) => k !== 'Cancelled' && k !== 'Unknown'))('%s has an Italian text', (kind) => {
    expect(describeError({ kind, message: 'raw', stderr: '' }).title).not.toBe('Errore');
  });
  it('untracked files in the way never propose an action', () => {
    expect(describeError({ kind: 'UntrackedWouldBeOverwritten', message: '', stderr: '' }).action).toBeNull();
  });
});
