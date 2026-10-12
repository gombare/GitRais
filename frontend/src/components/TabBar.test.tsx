// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';

vi.mock('../api/client', async (orig) => {
  const m = await orig<typeof import('../api/client')>();
  return { ...m, api: { SetActiveTab: vi.fn(async () => undefined), CloseTab: vi.fn(async () => undefined), GetState: vi.fn(async () => ({})) } };
});

import { useApp } from '../store/app';
import { TabBar } from './TabBar';
import { Welcome } from './Welcome';

const tab = (id: string) => ({ id, path: `/r/${id}`, name: id });

beforeEach(() => useApp.setState({ tabs: [tab('a'), tab('b')], active: 'a' }));
afterEach(cleanup);

describe('TabBar', () => {
  it('exposes tablist/tab roles with aria-selected', () => {
    render(<TabBar />);
    expect(screen.getByRole('tablist')).toBeTruthy();
    const tabs = screen.getAllByRole('tab');
    expect(tabs.map((t) => t.getAttribute('aria-selected'))).toEqual(['true', 'false']);
    expect(screen.getByRole('button', { name: 'Chiudi a' })).toBeTruthy();
  });
  it('moves selection with arrow keys (wrapping)', () => {
    render(<TabBar />);
    fireEvent.keyDown(screen.getAllByRole('tab')[0], { key: 'ArrowRight' });
    expect(useApp.getState().active).toBe('b');
    fireEvent.keyDown(screen.getAllByRole('tab')[1], { key: 'ArrowRight' });
    expect(useApp.getState().active).toBe('a');
    fireEvent.keyDown(screen.getAllByRole('tab')[0], { key: 'ArrowLeft' });
    expect(useApp.getState().active).toBe('b');
  });
});

describe('Welcome favourites', () => {
  it('star buttons expose aria-pressed', () => {
    useApp.setState({ state: { recents: ['/x/y', 'C:\\z\\w'], favorites: ['/x/y'] } as never, startupError: '' });
    render(<Welcome />);
    const stars = screen.getAllByRole('button', { name: 'Preferito' });
    expect(stars.map((s) => s.getAttribute('aria-pressed'))).toContain('true');
    expect(stars.map((s) => s.getAttribute('aria-pressed'))).toContain('false');
    expect(screen.getAllByText('w').length).toBeGreaterThan(0);
  });
});
