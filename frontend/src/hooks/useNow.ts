import { useEffect, useState } from 'react';

// Current time in seconds since the epoch, refreshed every `intervalMs` (for relative dates).
export function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now() / 1000), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}
