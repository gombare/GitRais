// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, renderHook } from '@testing-library/react';
import { useDevicePixelRatio } from './useDevicePixelRatio';

type Mql = { media: string; listeners: Set<() => void>; addEventListener: (t: string, f: () => void) => void; removeEventListener: (t: string, f: () => void) => void };

function mockMatchMedia() {
  const list: Mql[] = [];
  window.matchMedia = vi.fn((media: string) => {
    const m: Mql = {
      media, listeners: new Set(),
      addEventListener: (_t, f) => m.listeners.add(f),
      removeEventListener: (_t, f) => m.listeners.delete(f),
    };
    list.push(m);
    return m as unknown as MediaQueryList;
  });
  return list;
}

afterEach(() => cleanup());

describe('useDevicePixelRatio', () => {
  it('follows dpr changes, re-arming the listener each time, and cleans up', () => {
    window.devicePixelRatio = 1;
    const list = mockMatchMedia();
    const { result, unmount } = renderHook(() => useDevicePixelRatio());
    expect(result.current).toBe(1);
    expect(list[0].media).toBe('(resolution: 1dppx)');
    act(() => {
      window.devicePixelRatio = 2;
      [...list[0].listeners].forEach((f) => f());
    });
    expect(result.current).toBe(2);
    expect(list[0].listeners.size).toBe(0);
    expect(list[1].media).toBe('(resolution: 2dppx)');
    expect(list[1].listeners.size).toBe(1);
    unmount();
    expect(list[1].listeners.size).toBe(0);
  });
});
