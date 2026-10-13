// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({
  PickDirectory: vi.fn(async (..._a: unknown[]) => '/parent'),
  InitRepo: vi.fn(async (..._a: unknown[]) => ({ id: 'n', path: '/parent/new', name: 'new' })),
  CloneRepo: vi.fn(async (..._a: unknown[]) => ({ id: 'c', path: '/parent/repo', name: 'repo' })),
  GetState: vi.fn(async () => ({ activeTab: '', recents: ['/parent/new'], favorites: [] })),
  CancelClone: vi.fn(async () => undefined),
  ToggleFavorite: vi.fn(async () => undefined),
}));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { Welcome } from './Welcome';

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockClear());
  useApp.setState({ tabs: [], active: null, toasts: [], ops: {}, writes: {}, state: { activeTab: '', recents: [], favorites: [] } as never });
});
afterEach(cleanup);

describe('Welcome', () => {
  it('reloads recents/favorites after InitRepo', async () => {
    render(<Welcome />);
    fireEvent.click(screen.getByRole('button', { name: 'Nuovo repository…' }));
    await waitFor(() => expect(useApp.getState().state?.recents).toEqual(['/parent/new']));
    expect(api.InitRepo).toHaveBeenCalledWith('/parent');
    expect(useApp.getState().active).toBe('n');
  });
  it('reloads recents/favorites after CloneRepo', async () => {
    render(<Welcome />);
    fireEvent.change(screen.getByLabelText('URL da clonare'), { target: { value: 'https://x/repo.git' } });
    fireEvent.click(screen.getByRole('button', { name: 'Clona…' }));
    await waitFor(() => expect(api.GetState).toHaveBeenCalled());
    expect(api.CloneRepo).toHaveBeenCalledWith('https://x/repo.git', '/parent');
    expect(useApp.getState().active).toBe('c');
  });
});
