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
    expect(d.detail).toContain('lint failed');
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
