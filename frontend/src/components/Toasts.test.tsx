// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({ Pull: vi.fn(async (..._a: unknown[]) => undefined), RemoveIndexLock: vi.fn(async (..._a: unknown[]) => undefined) }));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { Toasts } from './Toasts';
import { DialogHost } from './Dialogs';

beforeEach(() => {
  api.Pull.mockReset().mockResolvedValue(undefined);
  api.RemoveIndexLock.mockReset().mockResolvedValue(undefined);
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [], ops: {} });
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
  it('a successful removal tells the user to retry', async () => {
    render(<><Toasts /><DialogHost /></>);
    fireEvent.click(screen.getByRole('button', { name: 'Rimuovi lock' }));
    const dlg = await screen.findByRole('dialog');
    await act(async () => { fireEvent.click(dlg.querySelector('button.danger')!); });
    const done = await screen.findByText(/Lock rimosso/);
    expect(done.closest('.toast')!.className).toContain('toast-success');
  });
  it('the Pull action ignores a double activation', async () => {
    useApp.setState({ toasts: [] });
    act(() => useApp.getState().toast(JSON.stringify({ kind: 'NonFastForward', message: 'm', stderr: '' }), 't'));
    api.Pull.mockImplementation(() => new Promise<undefined>(() => {}));
    render(<Toasts />);
    const b = screen.getByRole('button', { name: 'Pull' });
    fireEvent.click(b);
    fireEvent.click(b);
    await waitFor(() => expect(api.Pull).toHaveBeenCalled());
    expect(api.Pull).toHaveBeenCalledTimes(1);
  });
  it('the Pull action uses the toolbar flow: PullStrategyRequired asks for a strategy', async () => {
    useApp.setState({ toasts: [] });
    act(() => useApp.getState().toast(JSON.stringify({ kind: 'NonFastForward', message: 'm', stderr: '' }), 't'));
    api.Pull.mockRejectedValueOnce(new Error(JSON.stringify({ kind: 'PullStrategyRequired', message: 'x', stderr: '' })));
    render(<><Toasts /><DialogHost /></>);
    fireEvent.click(screen.getByRole('button', { name: 'Pull' }));
    await screen.findByRole('dialog');
    expect(useApp.getState().toasts).toHaveLength(0);
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Merge' })); });
    await waitFor(() => expect(api.Pull).toHaveBeenLastCalledWith('t', 'merge'));
    expect(useApp.getState().toasts).toHaveLength(0);
  });
});

const err = (kind: string, stderr = '') => JSON.stringify({ kind, message: 'm', stderr });

describe('Toasts presentation', () => {
  beforeEach(() => useApp.setState({ toasts: [] }));
  afterEach(() => vi.useRealTimers());

  it('marks the severity: errors are alerts, notices are status messages', () => {
    act(() => {
      useApp.getState().toast(err('AuthFailed'), 't');
      useApp.getState().notify({ severity: 'success', title: 'Fatto', tab: 't' });
    });
    render(<Toasts />);
    const e = screen.getByText('Autenticazione fallita').closest('.toast')!;
    const ok = screen.getByText('Fatto').closest('.toast')!;
    expect(e.className).toContain('toast-error');
    expect(e.getAttribute('role')).toBe('alert');
    expect(ok.className).toContain('toast-success');
    expect(ok.getAttribute('role')).toBe('status');
  });

  it('keeps git stderr collapsed under "Dettagli"', () => {
    act(() => useApp.getState().toast(err('DirtyWorktree', 'error: Your local changes would be overwritten'), 't'));
    render(<Toasts />);
    const details = screen.getByText('Dettagli').closest('details')!;
    expect(details.open).toBe(false);
    expect(details.textContent).toContain('would be overwritten');
  });

  it('auto-dismisses success and info after 5 s; errors stay', () => {
    vi.useFakeTimers();
    act(() => {
      useApp.getState().notify({ severity: 'success', title: 'Fatto', tab: 't' });
      useApp.getState().notify({ severity: 'info', title: 'Nota' });
      useApp.getState().toast(err('AuthFailed'), 't');
    });
    render(<Toasts />);
    act(() => { vi.advanceTimersByTime(4900); });
    expect(screen.queryByText('Fatto')).not.toBeNull();
    act(() => { vi.advanceTimersByTime(200); });
    expect(screen.queryByText('Fatto')).toBeNull();
    expect(screen.queryByText('Nota')).toBeNull();
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(screen.queryByText('Autenticazione fallita')).not.toBeNull();
  });

  it('pauses the auto-dismiss while hovered or focused', () => {
    vi.useFakeTimers();
    act(() => useApp.getState().notify({ severity: 'success', title: 'Fatto', tab: 't' }));
    render(<Toasts />);
    const el = screen.getByText('Fatto').closest('.toast')!;
    fireEvent.mouseEnter(el);
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(screen.queryByText('Fatto')).not.toBeNull();
    fireEvent.mouseLeave(el);
    fireEvent.focus(screen.getByRole('button', { name: 'Chiudi' }));
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(screen.queryByText('Fatto')).not.toBeNull();
    fireEvent.blur(screen.getByRole('button', { name: 'Chiudi' }));
    act(() => { vi.advanceTimersByTime(5_100); });
    expect(screen.queryByText('Fatto')).toBeNull();
  });

  it('shows at most 3 toasts; the oldest collapse into "+N"', () => {
    act(() => {
      for (let i = 1; i <= 5; i++) useApp.getState().toast(JSON.stringify({ kind: 'Unknown', message: `e${i}`, stderr: '' }), 't');
    });
    render(<Toasts />);
    expect(screen.queryByText('e1')).toBeNull();
    expect(screen.queryByText('e2')).toBeNull();
    expect(screen.getByText('e5')).toBeTruthy();
    expect(document.querySelectorAll('.toast')).toHaveLength(3);
    fireEvent.click(screen.getByRole('button', { name: /Mostra altre 2 notifiche/ }));
    expect(document.querySelectorAll('.toast')).toHaveLength(5);
  });

  it('shows only the active tab toasts, plus global ones', () => {
    act(() => {
      useApp.getState().toast(JSON.stringify({ kind: 'Unknown', message: 'mine', stderr: '' }), 't');
      useApp.getState().toast(JSON.stringify({ kind: 'Unknown', message: 'other', stderr: '' }), 'u');
      useApp.getState().toast(JSON.stringify({ kind: 'Unknown', message: 'global', stderr: '' }));
    });
    render(<Toasts />);
    expect(screen.getByText('mine')).toBeTruthy();
    expect(screen.getByText('global')).toBeTruthy();
    expect(screen.queryByText('other')).toBeNull();
    act(() => useApp.setState({ active: null }));
    expect(screen.queryByText('mine')).toBeNull();
    expect(screen.getByText('global')).toBeTruthy();
  });
});
