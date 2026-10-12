// Commit message helpers. Git keeps "#" lines when the message comes from a file (-F),
// so nothing here adds or removes comment lines.

export function buildMessage(summary: string, body: string): string {
  const s = summary.trim();
  const b = body.trim();
  return b ? `${s}\n\n${b}` : s;
}

// Splits a stored message into summary (first non-blank line) and body (the rest,
// without surrounding blank lines). Tolerates CRLF and leading blank lines.
export function splitMessage(msg: string): { summary: string; body: string } {
  const lines = msg.replace(/\r\n?/g, '\n').split('\n');
  let i = 0;
  while (i < lines.length && lines[i].trim() === '') i++;
  if (i === lines.length) return { summary: '', body: '' };
  const summary = lines[i].trim();
  const rest = lines.slice(i + 1);
  while (rest.length && rest[0].trim() === '') rest.shift();
  return { summary, body: rest.join('\n').trimEnd() };
}
