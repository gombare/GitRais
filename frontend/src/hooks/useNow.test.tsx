// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, render, screen } from '@testing-library/react';
import { useNow } from './useNow';

function Show() {
  return <span>{Math.floor(useNow(60_000))}</span>;
}

afterEach(() => { cleanup(); vi.useRealTimers(); });

describe('useNow', () => {
  it('refreshes on its interval, and at once on window focus or when the page becomes visible', () => {
    vi.useFakeTimers();
    vi.setSystemTime(1_000_000_000);
    render(<Show />);
    expect(screen.getByText('1000000')).toBeTruthy();
    vi.setSystemTime(1_000_030_000); // 30 s later, before the next tick
    act(() => { window.dispatchEvent(new Event('focus')); });
    expect(screen.getByText('1000030')).toBeTruthy();
    vi.setSystemTime(1_000_040_000);
    act(() => { document.dispatchEvent(new Event('visibilitychange')); });
    expect(screen.getByText('1000040')).toBeTruthy();
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(screen.getByText('1000100')).toBeTruthy();
  });
});
