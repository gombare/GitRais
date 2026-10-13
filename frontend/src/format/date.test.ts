import { describe, expect, it } from 'vitest';
import { fullDate, relativeDate } from './date';

// Local-time helper: seconds since the epoch for a local calendar date.
const at = (y: number, mo: number, d: number, h = 12, mi = 0, s = 0) => new Date(y, mo - 1, d, h, mi, s).getTime() / 1000;
const NOW = at(2026, 10, 13, 15, 30);

describe('relativeDate', () => {
  it('says "adesso" under a minute, and for small clock skews into the future', () => {
    expect(relativeDate(NOW - 20, NOW)).toBe('adesso');
    expect(relativeDate(NOW + 30, NOW)).toBe('adesso');
  });
  it('counts minutes under an hour', () => {
    expect(relativeDate(NOW - 60, NOW)).toBe('1 min fa');
    expect(relativeDate(NOW - 59 * 60, NOW)).toBe('59 min fa');
  });
  it('counts hours on the same day', () => {
    expect(relativeDate(NOW - 3600, NOW)).toBe('1 ora fa');
    expect(relativeDate(NOW - 3 * 3600, NOW)).toBe('3 ore fa');
  });
  it('says "ieri" for the previous calendar day', () => {
    expect(relativeDate(at(2026, 10, 12, 23, 50), NOW)).toBe('ieri');
    expect(relativeDate(at(2026, 10, 12, 0, 5), NOW)).toBe('ieri');
  });
  it('counts days within a week', () => {
    expect(relativeDate(at(2026, 10, 10, 9), NOW)).toBe('3 giorni fa');
  });
  it('uses a short Italian date in the same year, and adds the year otherwise', () => {
    expect(relativeDate(at(2026, 3, 2), NOW)).toBe('2 mar');
    expect(relativeDate(at(2024, 10, 12), NOW)).toBe('12 ott 2024');
  });
  it('shows an absolute date for timestamps well in the future', () => {
    expect(relativeDate(at(2026, 12, 25), NOW)).toBe('25 dic');
  });
});

describe('fullDate', () => {
  it('formats with the it-IT locale', () => {
    expect(fullDate(at(2026, 10, 13, 22, 17, 9))).toBe('13/10/2026, 22:17:09');
  });
});
