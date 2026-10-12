// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({ RemoveIndexLock: vi.fn(async (..._a: unknown[]) => undefined) }));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { Toasts } from './Toasts';
import { DialogHost } from './Dialogs';

beforeEach(() => {
  api.RemoveIndexLock.mockReset().mockResolvedValue(undefined);
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], toasts: [], ops: {} });
  act(() => useApp.getState().toast(JSON.stringify({ kind: 'Locked', message: 'm', stderr: '' }), 't'));
});
afterEach(cleanup);

describe('Toasts removeLock', () => {
  it('asks for confirmation before removing the lock', async () => {
    render(<><Toasts /><DialogHost /></>);
    fireEvent.click(screen.getByRole('button', { name: 'Rimuovi lock' }));
    const dlg = await screen.findByRole('dialog');
    expect(api.RemoveIndexLock).not.toHaveBeenCalled();
    await act(async () => { fireEvent.click(dlg.querySelector('button.danger')!); });
    await waitFor(() => expect(api.RemoveIndexLock).toHaveBeenCalledWith('t'));
  });
  it('does nothing when the user cancels', async () => {
    render(<><Toasts /><DialogHost /></>);
    fireEvent.click(screen.getByRole('button', { name: 'Rimuovi lock' }));
    await screen.findByRole('dialog');
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Annulla' })); });
    expect(api.RemoveIndexLock).not.toHaveBeenCalled();
  });
  it('a refused removal (lock in use) shows its own toast', async () => {
    api.RemoveIndexLock.mockRejectedValue(new Error(JSON.stringify({ kind: 'LockInUse', message: 'x', stderr: '' })));
    render(<><Toasts /><DialogHost /></>);
    fireEvent.click(screen.getByRole('button', { name: 'Rimuovi lock' }));
    const dlg = await screen.findByRole('dialog');
    await act(async () => { fireEvent.click(dlg.querySelector('button.danger')!); });
    await screen.findByText('Lock in uso');
  });
});
