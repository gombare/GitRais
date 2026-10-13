// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';

const api = vi.hoisted(() => ({ AnswerAuth: vi.fn(async (..._a: unknown[]) => undefined) }));
vi.mock('../api/client', async (orig) => ({ ...(await orig<typeof import('../api/client')>()), api }));

import { useApp } from '../store/app';
import { AuthDialog } from './AuthDialog';

const push = (p: { id: string; prompt: string; secret: boolean }) => act(() => useApp.getState().pushAuth(p));

beforeEach(() => {
  api.AnswerAuth.mockClear();
  useApp.setState({ authQueue: [] });
});
afterEach(cleanup);

describe('AuthDialog', () => {
  it('renders nothing without a prompt', () => {
    render(<AuthDialog />);
    expect(screen.queryByRole('dialog')).toBeNull();
  });
  it('secret prompts use a password field, focused, with the keychain note', () => {
    render(<AuthDialog />);
    push({ id: 'a', prompt: "Password for 'https://x@host': ", secret: true });
    const input = screen.getByLabelText(/Password for/) as HTMLInputElement;
    expect(input.type).toBe('password');
    expect(input.autocomplete).toBe('off');
    expect(document.activeElement).toBe(input);
    expect(screen.getByRole('dialog').getAttribute('aria-modal')).toBe('true');
    expect(screen.getByRole('dialog').textContent).toContain('Portachiavi');
  });
  it('non-secret prompts use a text field', () => {
    render(<AuthDialog />);
    push({ id: 'a', prompt: "Username for 'https://host': ", secret: false });
    expect((screen.getByLabelText(/Username/) as HTMLInputElement).type).toBe('text');
  });
  it('submit answers with ok=true and closes', () => {
    render(<AuthDialog />);
    push({ id: 'a', prompt: 'Password: ', secret: true });
    fireEvent.change(screen.getByLabelText(/Password/), { target: { value: 's3cret' } });
    fireEvent.submit(screen.getByRole('dialog'));
    expect(api.AnswerAuth).toHaveBeenCalledWith('a', 's3cret', true);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(useApp.getState().authQueue).toHaveLength(0);
  });
  it('cancel button and Escape answer ok=false with an empty value', () => {
    render(<AuthDialog />);
    push({ id: 'a', prompt: 'Password: ', secret: true });
    fireEvent.change(screen.getByLabelText(/Password/), { target: { value: 'typed' } });
    fireEvent.click(screen.getByRole('button', { name: 'Annulla' }));
    expect(api.AnswerAuth).toHaveBeenLastCalledWith('a', '', false);
    push({ id: 'b', prompt: 'Password: ', secret: true });
    fireEvent.keyDown(screen.getByLabelText(/Password/), { key: 'Escape' });
    expect(api.AnswerAuth).toHaveBeenLastCalledWith('b', '', false);
    expect(api.AnswerAuth).toHaveBeenCalledTimes(2);
  });
  it('auth:cancel (store cancelAuth) closes the dialog without answering', () => {
    render(<AuthDialog />);
    push({ id: 'a', prompt: 'Password: ', secret: true });
    act(() => useApp.getState().cancelAuth('a'));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(api.AnswerAuth).not.toHaveBeenCalled();
  });
  it('shows queued prompts one at a time with a clean input', () => {
    render(<AuthDialog />);
    push({ id: 'a', prompt: 'Username: ', secret: false });
    push({ id: 'b', prompt: 'Password: ', secret: true });
    fireEvent.change(screen.getByLabelText(/Username/), { target: { value: 'me' } });
    fireEvent.submit(screen.getByRole('dialog'));
    const next = screen.getByLabelText(/Password/) as HTMLInputElement;
    expect(next.value).toBe('');
    expect(document.activeElement).toBe(next);
  });
  it('a double submit answers once', () => {
    render(<AuthDialog />);
    push({ id: 'a', prompt: 'Password: ', secret: true });
    const form = screen.getByRole('dialog');
    fireEvent.submit(form);
    fireEvent.submit(form);
    expect(api.AnswerAuth).toHaveBeenCalledTimes(1);
  });
  it('traps Tab focus inside the dialog', () => {
    render(<AuthDialog />);
    push({ id: 'a', prompt: 'Password: ', secret: true });
    const ok = screen.getByRole('button', { name: 'Invia' });
    ok.focus();
    fireEvent.keyDown(ok, { key: 'Tab' });
    expect(document.activeElement).toBe(screen.getByLabelText(/Password/));
    fireEvent.keyDown(screen.getByLabelText(/Password/), { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(ok);
  });
  it('restores focus to the previously focused element', () => {
    render(<><button>opener</button><AuthDialog /></>);
    screen.getByText('opener').focus();
    push({ id: 'a', prompt: 'Password: ', secret: true });
    fireEvent.click(screen.getByRole('button', { name: 'Annulla' }));
    expect(document.activeElement).toBe(screen.getByText('opener'));
  });
  it('a rejected AnswerAuth becomes a generic toast that never contains the value', async () => {
    api.AnswerAuth.mockRejectedValueOnce(new Error('failed for s3cret'));
    useApp.setState({ toasts: [] });
    render(<AuthDialog />);
    push({ id: 'a', prompt: 'Password: ', secret: true });
    fireEvent.change(screen.getByLabelText(/Password/), { target: { value: 's3cret' } });
    fireEvent.submit(screen.getByRole('dialog'));
    await waitFor(() => expect(useApp.getState().toasts).toHaveLength(1));
    expect(JSON.stringify(useApp.getState().toasts)).not.toContain('s3cret');
  });
});
