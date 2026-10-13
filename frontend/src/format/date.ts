// Commit date formatting for the UI (Italian).

const short = new Intl.DateTimeFormat('it-IT', { day: 'numeric', month: 'short' });
const shortYear = new Intl.DateTimeFormat('it-IT', { day: 'numeric', month: 'short', year: 'numeric' });

// Intl may append a dot to abbreviated months on some ICU versions ("ott."): keep the compact form.
const clean = (s: string) => s.replace(/\./g, '');

const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();

// Full local date and time, e.g. "13/10/2026, 22:17:09" (used for tooltips and details).
export function fullDate(t: number): string {
  return new Date(t * 1000).toLocaleString('it-IT');
}

// Compact relative date: "adesso", "5 min fa", "3 ore fa", "ieri", "4 giorni fa", "12 ott", "12 ott 2024".
// `t` and `now` are seconds since the epoch.
export function relativeDate(t: number, now: number = Date.now() / 1000): string {
  const diff = now - t;
  const date = new Date(t * 1000);
  const today = new Date(now * 1000);
  if (diff < 60 && diff > -60) return 'adesso';
  if (diff > 0) {
    if (diff < 3600) return `${Math.floor(diff / 60)} min fa`;
    const days = Math.round((dayStart(today) - dayStart(date)) / 86400000);
    if (days === 0) {
      const h = Math.floor(diff / 3600);
      return h === 1 ? '1 ora fa' : `${h} ore fa`;
    }
    if (days === 1) return 'ieri';
    if (days < 7) return `${days} giorni fa`;
  }
  return clean(date.getFullYear() === today.getFullYear() ? short.format(date) : shortYear.format(date));
}
