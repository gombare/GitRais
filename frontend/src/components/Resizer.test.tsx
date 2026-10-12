// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { Resizer } from './Resizer';

afterEach(cleanup);

const mount = (invert = false) => {
  const onResize = vi.fn();
  const onDone = vi.fn();
  render(<Resizer label="Larghezza" value={260} min={180} max={600} invert={invert} onResize={onResize} onDone={onDone} />);
  return { onResize, onDone, sep: screen.getByRole('separator', { name: 'Larghezza' }) };
};

describe('Resizer', () => {
  it('exposes the value to assistive tech and is focusable', () => {
    const { sep } = mount();
    expect(sep.getAttribute('aria-valuenow')).toBe('260');
    expect(sep.getAttribute('aria-valuemin')).toBe('180');
    expect(sep.getAttribute('aria-valuemax')).toBe('600');
    expect(sep.tabIndex).toBe(0);
  });
  it('arrow keys resize and persist; right panels are inverted', () => {
    const l = mount();
    fireEvent.keyDown(l.sep, { key: 'ArrowRight' });
    expect(l.onResize).toHaveBeenCalledWith(16);
    expect(l.onDone).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(l.sep, { key: 'ArrowLeft', shiftKey: true });
    expect(l.onResize).toHaveBeenLastCalledWith(-64);
    cleanup();
    const r = mount(true);
    fireEvent.keyDown(r.sep, { key: 'ArrowRight' });
    expect(r.onResize).toHaveBeenCalledWith(-16);
  });
  it('Home/End jump to the limits; other keys are ignored', () => {
    const { sep, onResize, onDone } = mount();
    fireEvent.keyDown(sep, { key: 'End' });
    expect(onResize).toHaveBeenLastCalledWith(340);
    fireEvent.keyDown(sep, { key: 'Home' });
    expect(onResize).toHaveBeenLastCalledWith(-80);
    fireEvent.keyDown(sep, { key: 'a' });
    expect(onDone).toHaveBeenCalledTimes(2);
  });
  it('dragging reports deltas and calls onDone on mouseup', () => {
    const { sep, onResize, onDone } = mount();
    fireEvent.mouseDown(sep, { clientX: 100 });
    fireEvent.mouseMove(window, { clientX: 130 });
    expect(onResize).toHaveBeenLastCalledWith(30);
    fireEvent.mouseUp(window);
    expect(onDone).toHaveBeenCalledTimes(1);
    fireEvent.mouseMove(window, { clientX: 200 });
    expect(onResize).toHaveBeenCalledTimes(1);
  });
});
