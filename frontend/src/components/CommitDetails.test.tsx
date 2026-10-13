// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({ CommitDetail: vi.fn(async (..._a: unknown[]): Promise<any> => undefined), FindRow: vi.fn(async (..._a: unknown[]) => 3) }));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { useRepo } from '../store/repo';
import { COPIED_MS, CommitDetails } from './CommitDetails';

const H1 = '1'.repeat(40), H2 = '2'.repeat(40), P = '9'.repeat(40);
const detail = (hash: string, summary: string) => ({
  hash, summary, body: 'the body', parents: [P], author: 'Ann', authorEmail: 'a@x', authorTime: 1700000000,
  committer: 'Ann', committerEmail: 'a@x', commitTime: 1700000000,
  files: [{ path: 'n.go', oldPath: 'o.go', status: 'R', additions: 2, deletions: 1, binary: false }, { path: 'i.png', oldPath: '', status: 'A', additions: 0, deletions: 0, binary: true }],
});

beforeEach(() => {
  api.CommitDetail.mockReset();
  useApp.setState({ tabs: [{ id: 't', path: '/r', name: 'r' }], active: 't', toasts: [] });
  useRepo.setState({ repos: { t: { stashes: [], graphRows: 0, graphDone: true, graphVersion: 0, selection: { kind: 'commit', hash: H1 }, diff: null } } });
});
afterEach(cleanup);

describe('CommitDetails', () => {
  it('shows metadata, message and files; a file click opens the commit diff', async () => {
    api.CommitDetail.mockResolvedValue(detail(H1, 'subject'));
    render(<CommitDetails tab="t" hash={H1} />);
    await screen.findByText('subject');
    expect(screen.getByText('the body')).toBeTruthy();
    expect(screen.getByText(/Ann <a@x>/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /n\.go/ }));
    expect(useRepo.getState().repos.t.diff).toEqual({ kind: 'commit', hash: H1, path: 'n.go', oldPath: 'o.go' });
  });
  it('parent button jumps to the parent', async () => {
    api.CommitDetail.mockResolvedValue(detail(H1, 'subject'));
    render(<CommitDetails tab="t" hash={H1} />);
    await screen.findByText('subject');
    fireEvent.click(screen.getByRole('button', { name: P.slice(0, 7) }));
    await waitFor(() => expect(useRepo.getState().repos.t.selection).toEqual({ kind: 'commit', hash: P }));
  });
  it('ignores a stale response when the selection changed', async () => {
    let r1: (v: any) => void = () => {};
    api.CommitDetail.mockImplementationOnce(() => new Promise((r) => { r1 = r; }));
    api.CommitDetail.mockImplementationOnce(async () => detail(H2, 'second'));
    const { rerender } = render(<CommitDetails tab="t" hash={H1} />);
    rerender(<CommitDetails tab="t" hash={H2} />);
    await screen.findByText('second');
    r1(detail(H1, 'first'));
    await new Promise((r) => setTimeout(r, 10));
    expect(screen.queryByText('first')).toBeNull();
    expect(screen.getByText('second')).toBeTruthy();
  });
  it('copying the hash shows a brief "Copiato ✓"', async () => {
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    api.CommitDetail.mockResolvedValue(detail(H1, 'subject'));
    render(<CommitDetails tab="t" hash={H1} />);
    await screen.findByText('subject');
    const status = screen.getByRole('status');
    expect(status.textContent).toBe('');
    fireEvent.click(screen.getByRole('button', { name: `Copia hash ${H1}` }));
    expect(writeText).toHaveBeenCalledWith(H1);
    await waitFor(() => expect(status.textContent).toBe('Copiato ✓'));
    await waitFor(() => expect(status.textContent).toBe(''), { timeout: COPIED_MS + 1000 });
  });
  it('a failed copy becomes a toast, without the confirmation', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn(async () => { throw new Error('denied'); }) }, configurable: true });
    api.CommitDetail.mockResolvedValue(detail(H1, 'subject'));
    render(<CommitDetails tab="t" hash={H1} />);
    await screen.findByText('subject');
    fireEvent.click(screen.getByRole('button', { name: `Copia hash ${H1}` }));
    await waitFor(() => expect(useApp.getState().toasts).toHaveLength(1));
    expect(screen.getByRole('status').textContent).toBe('');
  });
  it('toasts on load error', async () => {
    api.CommitDetail.mockRejectedValue(JSON.stringify({ kind: 'InvalidHash', message: '', stderr: '' }));
    render(<CommitDetails tab="t" hash={H1} />);
    await waitFor(() => expect(useApp.getState().toasts[0]?.error.kind).toBe('InvalidHash'));
  });
});
