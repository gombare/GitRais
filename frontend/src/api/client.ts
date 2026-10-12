import * as App from '../../wailsjs/go/app/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';

// Mirrors the backend UIError DTO (internal/app). Rejections of bound methods carry it as a JSON string.
export type UIError = { kind: string; message: string; stderr: string };

// Payload of the `auth:prompt` event (app.AuthPrompt is not part of the generated models).
export type AuthPrompt = { id: string; prompt: string; secret: boolean };

function stringify(e: unknown): string {
  try {
    return JSON.stringify(e) ?? String(e);
  } catch {
    return String(e);
  }
}

export function parseError(e: unknown): UIError {
  const s = typeof e === 'string' ? e : e instanceof Error ? e.message : stringify(e);
  try {
    const o = JSON.parse(s);
    if (o && typeof o.kind === 'string') return { kind: o.kind, message: o.message ?? '', stderr: o.stderr ?? '' };
  } catch {
    /* not JSON: plain text error */
  }
  return { kind: 'Unknown', message: s, stderr: '' };
}

export const api = App;

export function on<T>(event: string, cb: (d: T) => void): () => void {
  return EventsOn(event, cb);
}
