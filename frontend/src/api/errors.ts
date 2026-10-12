import type { UIError } from './client';

export type ErrorAction = 'pull' | 'removeLock' | null;

const texts: Record<string, [string, string, ErrorAction]> = {
  AuthFailed: ['Autenticazione fallita', 'Credenziali rifiutate dal remote.', null],
  NonFastForward: ['Push rifiutato', 'Il remote contiene commit che non hai in locale. Esegui Pull e riprova.', 'pull'],
  Conflict: ['Conflitti', "L'operazione ha prodotto conflitti da risolvere.", null],
  DirtyWorktree: ['Modifiche locali', 'Le modifiche locali verrebbero sovrascritte: fai commit o stash.', null],
  Locked: ['Repository bloccato', 'Un altro processo git sta usando il repository (index.lock).', 'removeLock'],
  NotARepo: ['Non è un repository', 'La cartella selezionata non contiene un repository git.', null],
  Network: ['Errore di rete', 'Impossibile raggiungere il remote.', null],
  HookRejected: ['Hook rifiutato', 'Un hook git ha bloccato l’operazione.', null],
  EmptyMessage: ['Messaggio mancante', 'Scrivi un messaggio di commit.', null],
  GitMissing: ['Git non trovato', 'Il git incluso nell’app non è disponibile: le operazioni sui repository sono disabilitate.', null],
  Timeout: ['Tempo scaduto', 'L’operazione ha impiegato troppo tempo ed è stata interrotta.', null],
  InvalidPath: ['Percorso non valido', 'Il percorso deve essere assoluto e non vuoto.', null],
  InvalidFilePath: ['Percorso non valido', 'Il percorso del file non è valido o esce dal repository.', null],
  NoTab: ['Tab non trovata', 'La tab non esiste più.', null],
  OpInProgress: ['Operazione in corso', 'Un’altra operazione remota è già in esecuzione.', null],
  PullStrategyRequired: ['Strategia di pull richiesta', 'Le branch sono divergenti: scegli merge, rebase o solo fast-forward.', null],
  NothingToCommit: ['Niente da committare', 'Non ci sono modifiche in stage.', null],
  NothingToAmend: ['Niente da modificare', 'Non c’è un commit da emendare.', null],
  NothingToStash: ['Niente da mettere in stash', 'Non ci sono modifiche locali.', null],
  StashUnborn: ['Stash non possibile', 'Il repository non ha ancora commit.', null],
  StashNotFound: ['Stash non trovato', 'Lo stash non esiste più.', null],
  DestNotEmpty: ['Cartella non vuota', 'La cartella di destinazione esiste e non è vuota.', null],
  StateCorrupt: ['Stato corrotto', 'Il file di stato dell’app era danneggiato ed è stato ripristinato.', null],
  InvalidHash: ['Hash non valido', 'L’hash del commit non è valido.', null],
  PartialStageUnsupported: ['Stage parziale non supportato', 'Questo file non supporta lo stage di singole righe.', null],
  HunkOutOfRange: ['Hunk non valido', 'L’hunk selezionato non esiste più: aggiorna il diff.', null],
  LineOutOfRange: ['Riga non valida', 'La riga selezionata non esiste più: aggiorna il diff.', null],
  NoLinesSelected: ['Nessuna riga selezionata', 'Seleziona almeno una riga.', null],
  NoBranch: ['Nessuna branch', 'HEAD non è su una branch.', null],
  NoRemote: ['Nessun remote', 'Il repository non ha remote configurati.', null],
  UpstreamNotRemote: ['Upstream non valido', 'L’upstream deve essere una branch remota.', null],
};

// Cancelled covers user-cancelled ops, closed tabs and shutdown: never worth a toast.
export function isSilent(e: UIError): boolean {
  return e.kind === 'Cancelled';
}

export function describeError(e: UIError): { title: string; detail: string; action: ErrorAction } {
  const t = texts[e.kind];
  const extra = e.stderr.trim();
  if (!t) return { title: 'Errore', detail: extra || e.message, action: null };
  return { title: t[0], detail: extra ? `${t[1]}\n\n${extra}` : t[1], action: t[2] };
}
