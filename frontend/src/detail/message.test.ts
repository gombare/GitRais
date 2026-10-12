import { describe, expect, it } from 'vitest';
import { buildMessage, splitMessage } from './message';

describe('commit message', () => {
  it('joins summary and body with one blank line', () => {
    expect(buildMessage(' fix bug ', '\ndetails\n')).toBe('fix bug\n\ndetails');
    expect(buildMessage('only', '  ')).toBe('only');
  });
  it('never adds or strips comment lines', () => {
    expect(buildMessage('s', '# keep me')).toBe('s\n\n# keep me');
  });
  it('splits summary and body', () => {
    expect(splitMessage('a\n\nb\nc')).toEqual({ summary: 'a', body: 'b\nc' });
    expect(splitMessage('a')).toEqual({ summary: 'a', body: '' });
  });
  it('handles CRLF', () => {
    expect(splitMessage('a\r\n\r\nb\r\nc\r\n')).toEqual({ summary: 'a', body: 'b\nc' });
  });
  it('skips leading blank lines and trailing newline', () => {
    expect(splitMessage('\n\n  \na\n\nb\n')).toEqual({ summary: 'a', body: 'b' });
    expect(splitMessage('a\n')).toEqual({ summary: 'a', body: '' });
    expect(splitMessage('')).toEqual({ summary: '', body: '' });
  });
  it('keeps body indentation', () => {
    expect(splitMessage('a\n\n  indented\n    more')).toEqual({ summary: 'a', body: '  indented\n    more' });
  });
  it('round-trips', () => {
    const m = buildMessage('s', 'x\n\ny');
    expect(splitMessage(m)).toEqual({ summary: 's', body: 'x\n\ny' });
  });
});
