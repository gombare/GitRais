import { useEffect, useRef, useState } from 'react';
import { api, type AuthPrompt } from '../api/client';
import { useApp } from '../store/app';

const FOCUSABLE = 'button:not([disabled]), input:not([disabled])';

// Shows the head of the credential queue (the backend serialises prompts). The
// inner form is keyed by prompt id, so the typed value never survives an answer.
export function AuthDialog() {
  const p = useApp((s) => s.authQueue[0]);
  return p ? <PromptForm key={p.id} p={p} /> : null;
}

function PromptForm({ p }: { p: AuthPrompt }) {
  const cancelAuth = useApp((s) => s.cancelAuth);
  const toast = useApp((s) => s.toast);
  const [value, setValue] = useState('');
  const root = useRef<HTMLFormElement>(null);
  const answered = useRef(false);
  const opener = useRef<HTMLElement | null>(
    document.activeElement instanceof HTMLElement ? document.activeElement : null);

  useEffect(() => {
    const o = opener.current;
    return () => { if (o?.isConnected) o.focus(); };
  }, []);

  const answer = (ok: boolean) => {
    if (answered.current) return;
    answered.current = true;
    // The answer is never logged or stored: it only goes to the backend and the state is dropped.
    // A rejection gets a generic toast: the error text could echo the credential.
    api.AnswerAuth(p.id, ok ? value : '', ok).catch(() => toast(new Error('Impossibile inviare la risposta di autenticazione.'), undefined));
    setValue('');
    cancelAuth(p.id);
  };
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      answer(false);
    } else if (e.key === 'Tab') {
      const els = Array.from(root.current?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? []);
      if (!els.length) return;
      const first = els[0];
      const last = els[els.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  };
  return (
    <div className="modal-backdrop">
      <form ref={root} className="modal" role="dialog" aria-modal="true" aria-label="Credenziali richieste"
        onSubmit={(e) => { e.preventDefault(); answer(true); }} onKeyDown={onKeyDown}>
        <h3>Credenziali richieste</h3>
        <label>{p.prompt}
          <input autoFocus type={p.secret ? 'password' : 'text'} value={value} autoComplete="off"
            autoCorrect="off" autoCapitalize="off" spellCheck={false} onChange={(e) => setValue(e.target.value)} />
        </label>
        <p className="muted auth-note">
          Dopo l’accesso riuscito, le credenziali vengono salvate nel Portachiavi di macOS dal credential helper di git.
        </p>
        <div className="modal-actions">
          <button type="button" onClick={() => answer(false)}>Annulla</button>
          <button type="submit" className="primary">Invia</button>
        </div>
      </form>
    </div>
  );
}
