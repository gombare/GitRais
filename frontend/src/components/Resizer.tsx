const STEP = 16;

// Vertical splitter between two panels. `onResize(dx)` receives the width delta
// of the panel being resized (already inverted for a right-hand panel), and
// `onDone` fires when a drag or a key press has finished (time to persist).
// Keyboard: ArrowLeft/ArrowRight (Shift = larger step), Home/End jump to the limits.
export function Resizer({ label, value, min, max, onResize, onDone, invert = false }: {
  label: string; value: number; min: number; max: number;
  onResize: (dx: number) => void; onDone: () => void; invert?: boolean;
}) {
  const sign = invert ? -1 : 1;
  const down = (e: React.MouseEvent) => {
    e.preventDefault();
    let last = e.clientX;
    const move = (m: MouseEvent) => {
      onResize(sign * (m.clientX - last));
      last = m.clientX;
    };
    const up = () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
      onDone();
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
  };
  const key = (e: React.KeyboardEvent) => {
    const step = e.shiftKey ? STEP * 4 : STEP;
    let dx = 0;
    if (e.key === 'ArrowRight') dx = sign * step;
    else if (e.key === 'ArrowLeft') dx = -sign * step;
    else if (e.key === 'Home') dx = min - value;
    else if (e.key === 'End') dx = max - value;
    else return;
    e.preventDefault();
    onResize(dx);
    onDone();
  };
  return (
    <div className="resizer" role="separator" aria-orientation="vertical" aria-label={label} tabIndex={0}
      aria-valuenow={value} aria-valuemin={min} aria-valuemax={max} onMouseDown={down} onKeyDown={key} />
  );
}
