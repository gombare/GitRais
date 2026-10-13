// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { askText, askTextCheck, confirmAction, DialogHost } from './Dialogs';

afterEach(cleanup);

describe('Dialogs', () => {
  it('confirm: Enter on the focused OK confirms, focus returns to the opener', async () => {
    render(<><button>opener</button><DialogHost /></>);
    const opener = screen.getByText('opener');
    opener.focus();
    let p!: Promise<boolean>;
    act(() => { p = confirmAction({ title: 'T', message: 'M' }); });
    const ok = screen.getByRole('button', { name: 'OK' });
    expect(document.activeElement).toBe(ok);
    await act(async () => { fireEvent.click(ok); });
    await expect(p).resolves.toBe(true);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(document.activeElement).toBe(opener);
  });
  it('danger confirm: initial focus is Cancel, which cancels; OK stays reachable', async () => {
    render(<><button>opener</button><DialogHost /></>);
    const opener = screen.getByText('opener');
    opener.focus();
    let p!: Promise<boolean>;
    act(() => { p = confirmAction({ title: 'T', message: 'M', danger: true }); });
    const cancel = screen.getByRole('button', { name: 'Annulla' });
    expect(document.activeElement).toBe(cancel);
    expect(screen.getByRole('button', { name: 'OK' }).className).toBe('danger');
    await act(async () => { fireEvent.click(cancel); }); // Enter on a focused button fires click
    await expect(p).resolves.toBe(false);
    expect(document.activeElement).toBe(opener);
  });
  it('confirm: Escape cancels', async () => {
    render(<DialogHost />);
    let p!: Promise<boolean>;
    act(() => { p = confirmAction({ title: 'T', message: 'M' }); });
    await act(async () => { fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' }); });
    await expect(p).resolves.toBe(false);
  });
  it('text: Enter submits the trimmed value, empty value is not accepted', async () => {
    render(<DialogHost />);
    let p!: Promise<string | null>;
    act(() => { p = askText({ title: 'T', label: 'Nome', initial: '' }); });
    const input = screen.getByLabelText('Nome') as HTMLInputElement;
    expect(document.activeElement).toBe(input);
    expect((screen.getByRole('button', { name: 'OK' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(input, { target: { value: '  feat  ' } });
    await act(async () => { fireEvent.submit(input.closest('form')!); });
    await expect(p).resolves.toBe('feat');
  });
  it('text: Escape resolves null; backdrop click cancels', async () => {
    render(<DialogHost />);
    let p!: Promise<string | null>;
    act(() => { p = askText({ title: 'T', label: 'Nome', initial: 'x' }); });
    await act(async () => { fireEvent.keyDown(screen.getByLabelText('Nome'), { key: 'Escape' }); });
    await expect(p).resolves.toBeNull();
    act(() => { p = askText({ title: 'T', label: 'Nome', initial: 'x' }); });
    await act(async () => { fireEvent.mouseDown(document.querySelector('.modal-backdrop')!); });
    await expect(p).resolves.toBeNull();
  });
  it('traps Tab inside the dialog', () => {
    render(<><button>outside</button><DialogHost /></>);
    act(() => { void confirmAction({ title: 'T', message: 'M' }); });
    const ok = screen.getByRole('button', { name: 'OK' });
    const cancel = screen.getByRole('button', { name: 'Annulla' });
    ok.focus();
    fireEvent.keyDown(ok, { key: 'Tab' });
    expect(document.activeElement).toBe(cancel); // wraps from last to first
    fireEvent.keyDown(cancel, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(ok);
  });
  it('a new dialog cancels the one still open', async () => {
    render(<DialogHost />);
    let a!: Promise<boolean>;
    act(() => { a = confirmAction({ title: '1', message: 'm' }); });
    act(() => { void confirmAction({ title: '2', message: 'm' }); });
    await expect(a).resolves.toBe(false);
  });
});

describe('Dialogs: optional text and checkbox', () => {
  it('allowEmpty: OK with an empty value resolves "", Cancel resolves null', async () => {
    render(<DialogHost />);
    let p!: Promise<string | null>;
    act(() => { p = askText({ title: 'T', label: 'Messaggio', allowEmpty: true }); });
    expect((screen.getByRole('button', { name: 'OK' }) as HTMLButtonElement).disabled).toBe(false);
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'OK' })); });
    await expect(p).resolves.toBe('');
    act(() => { p = askText({ title: 'T', label: 'Messaggio', allowEmpty: true }); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Annulla' })); });
    await expect(p).resolves.toBeNull();
  });
  it('askTextCheck returns the text and a labelled, keyboard-reachable checkbox state', async () => {
    render(<DialogHost />);
    let p!: Promise<{ text: string; checked: boolean } | null>;
    act(() => { p = askTextCheck({ title: 'T', label: 'Nome', check: { label: 'Checkout', initial: true } }); });
    const box = screen.getByRole('checkbox', { name: 'Checkout' }) as HTMLInputElement;
    expect(box.checked).toBe(true);
    expect(box.tabIndex).not.toBe(-1);
    fireEvent.click(box);
    fireEvent.change(screen.getByLabelText('Nome'), { target: { value: 'feat' } });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'OK' })); });
    await expect(p).resolves.toEqual({ text: 'feat', checked: false });
  });
});

describe('Dialogs: action labels and item lists', () => {
  it('confirmLabel names the confirm button (confirm and text dialogs)', async () => {
    render(<DialogHost />);
    let p!: Promise<boolean>;
    act(() => { p = confirmAction({ title: 'T', message: 'M', danger: true, confirmLabel: 'Elimina tag' }); });
    expect(screen.queryByRole('button', { name: 'OK' })).toBeNull();
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Elimina tag' })); });
    await expect(p).resolves.toBe(true);
    let q!: Promise<string | null>;
    act(() => { q = askText({ title: 'T', label: 'Nome', initial: 'x', confirmLabel: 'Crea branch' }); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Crea branch' })); });
    await expect(q).resolves.toBe('x');
  });
  it('confirm items render as a list, one entry per line', () => {
    render(<DialogHost />);
    act(() => { void confirmAction({ title: 'T', message: 'M', items: ['a.txt', 'dir/b.txt'] }); });
    const list = screen.getByRole('list', { name: 'Elementi interessati' });
    expect(Array.from(list.querySelectorAll('li')).map((li) => li.textContent)).toEqual(['a.txt', 'dir/b.txt']);
    expect(list.className).toBe('modal-list');
  });
});
