import { useEffect, useState } from 'react';

// Tracks window.devicePixelRatio. The (resolution: Ndppx) media query only matches the current
// ratio, so its change event fires when the ratio moves away; the listener is re-armed for the new value.
export function useDevicePixelRatio(): number {
  const [dpr, setDpr] = useState(() => window.devicePixelRatio || 1);
  useEffect(() => {
    let mql: MediaQueryList | undefined;
    const arm = () => {
      const cur = window.devicePixelRatio || 1;
      setDpr(cur);
      mql = window.matchMedia(`(resolution: ${cur}dppx)`);
      mql.addEventListener('change', onChange);
    };
    const onChange = () => {
      mql?.removeEventListener('change', onChange);
      arm();
    };
    mql = window.matchMedia(`(resolution: ${window.devicePixelRatio || 1}dppx)`);
    mql.addEventListener('change', onChange);
    return () => mql?.removeEventListener('change', onChange);
  }, []);
  return dpr;
}
