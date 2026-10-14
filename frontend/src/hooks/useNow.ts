import { useEffect, useState } from 'react';

// Current time in seconds since the epoch, refreshed every `intervalMs` (for relative dates),
// and at once when the window regains focus or the page becomes visible again: timers are
// throttled or suspended while the app is in the background.
export function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const tick = () => setNow(Date.now() / 1000);
    const onVisible = () => { if (document.visibilityState !== 'hidden') tick(); };
    const id = window.setInterval(tick, intervalMs);
    window.addEventListener('focus', tick);
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      window.clearInterval(id);
      window.removeEventListener('focus', tick);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [intervalMs]);
  return now;
}
