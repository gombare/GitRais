// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({
  GetSettings: vi.fn(async (): Promise<unknown> => ({ autoFetch: true, autoFetchSeconds: 60 })),
  SetSettings: vi.fn(async (s: unknown) => s),
  AutoFetchStatus: vi.fn(async (..._a: unknown[]): Promise<unknown> => ({ enabled: true, intervalSeconds: 60, paused: false, reason: '' })),
}));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { AutoFetchSettings } from './AutoFetchSettings';

beforeEach(() => {
  api.GetSettings.mockReset().mockResolvedValue({ autoFetch: true, autoFetchSeconds: 60 });
  api.SetSettings.mockReset().mockImplementation(async (s: unknown) => s);
  api.AutoFetchStatus.mockReset().mockResolvedValue({ enabled: true, intervalSeconds: 60, paused: false, reason: '' });
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [], ops: {}, writes: {} });
  useRepo.setState({ repos: { t: { stashes: [], graphRows: 0, graphDone: true, graphVersion: 0, selection: { kind: 'wip' }, diff: null } } });
});
afterEach(cleanup);

const openPanel = async () => {
  render(<AutoFetchSettings tab="t" />);
  const btn = screen.getByRole('button', { name: /Fetch automatico/ });
  await act(async () => { fireEvent.click(btn); });
  return screen.findByRole('dialog', { name: 'Fetch automatico' });
};

describe('AutoFetchSettings', () => {
  it('toggles auto-fetch through SetSettings', async () => {
    await openPanel();
    const box = screen.getByRole('checkbox', { name: /Fetch automatico/ }) as HTMLInputElement;
    expect(box.checked).toBe(true);
    await act(async () => { fireEvent.click(box); });
    expect(api.SetSettings).toHaveBeenCalledWith({ autoFetch: false, autoFetchSeconds: 60 });
  });
  it('saves the interval on blur, never below 30 seconds', async () => {
    await openPanel();
    const input = screen.getByLabelText(/Intervallo/) as HTMLInputElement;
    fireEvent.change(input, { target: { value: '10' } });
    await act(async () => { fireEvent.blur(input); });
    expect(api.SetSettings).toHaveBeenLastCalledWith({ autoFetch: true, autoFetchSeconds: 30 });
    await waitFor(() => expect(input.value).toBe('30'));
  });
  it('shows when auto-fetch is paused for the tab', async () => {
    api.AutoFetchStatus.mockResolvedValue({ enabled: true, intervalSeconds: 60, paused: true, reason: 'auth' });
    render(<AutoFetchSettings tab="t" />);
    await waitFor(() => expect(screen.getByRole('button', { name: /in pausa/ })).toBeTruthy());
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /Fetch automatico/ })); });
    const dlg = await screen.findByRole('dialog');
    expect(dlg.textContent).toMatch(/credenziali/);
    expect(dlg.textContent).toMatch(/Fetch manuale/);
  });
  it('caps the interval at 3600 seconds', async () => {
    await openPanel();
    const input = screen.getByLabelText(/Intervallo/) as HTMLInputElement;
    expect(input.max).toBe('3600');
    fireEvent.change(input, { target: { value: '99999' } });
    await act(async () => { fireEvent.blur(input); });
    expect(api.SetSettings).toHaveBeenLastCalledWith({ autoFetch: true, autoFetchSeconds: 3600 });
  });
  it('follows autofetch:status live (via the repo store), with a label per reason', async () => {
    render(<AutoFetchSettings tab="t" />);
    const btn = () => screen.getByRole('button', { name: /Fetch automatico/ });
    await waitFor(() => expect(api.AutoFetchStatus).toHaveBeenCalled());
    act(() => useRepo.getState().setAutoFetch('t', { enabled: true, paused: true, reason: 'waiting-manual', lastFetch: 0 }));
    expect(btn().textContent).toMatch(/si attiva dopo il primo fetch manuale/);
    act(() => useRepo.getState().setAutoFetch('t', { enabled: true, paused: true, reason: 'auth', lastFetch: 0 }));
    expect(btn().textContent).toMatch(/in pausa: credenziali/);
    act(() => useRepo.getState().setAutoFetch('t', { enabled: true, paused: true, reason: 'network', lastFetch: 0 }));
    expect(btn().textContent).toMatch(/in pausa: rete/);
    act(() => useRepo.getState().setAutoFetch('t', { enabled: true, paused: false, reason: '', lastFetch: 5 }));
    expect(btn().textContent).toBe('Fetch automatico');
    act(() => useRepo.getState().setAutoFetch('t', { enabled: false, paused: true, reason: 'auth', lastFetch: 5 }));
    expect(btn().textContent).toBe('Fetch automatico (disattivato)');
  });
  it('Escape closes the panel and returns focus to the button', async () => {
    const dlg = await openPanel();
    fireEvent.keyDown(dlg, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole('button', { name: /Fetch automatico/ }));
  });
  it('a failed save becomes a toast', async () => {
    api.SetSettings.mockRejectedValueOnce(new Error(JSON.stringify({ kind: 'Unknown', message: 'disk full', stderr: '' })));
    await openPanel();
    await act(async () => { fireEvent.click(screen.getByRole('checkbox', { name: /Fetch automatico/ })); });
    expect(useApp.getState().toasts).toHaveLength(1);
  });
});
