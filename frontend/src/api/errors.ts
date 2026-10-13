import type { UIError } from './client';

export type ErrorAction = 'pull' | 'removeLock' | null;
export type Severity = 'error' | 'warning' | 'success' | 'info';

// Kinds that are not failures: notices and "nothing to do" outcomes. Everything else is an error.
const severities: Record<string, Severity> = {
  LockRemoved: 'success',
  NoLinesSelected: 'info',
  OpInProgress: 'info',
  NothingToCommit: 'info',
  NothingToAmend: 'info',
  NothingToStash: 'info',
  ClipboardUnavailable: 'warning',
  LockInUse: 'warning',
  LockTooRecent: 'warning',
  GitRunning: 'warning',
  LsofMissing: 'warning',
  StateCorrupt: 'warning',
  DiffChanged: 'warning',
};

const texts: Record<string, [string, string, ErrorAction]> = {
  AuthFailed: ['Autenticazione fallita', 'Credenziali rifiutate dal remote.', null],
  NonFastForward: ['Push rifiutato', 'Il remote contiene commit che non hai in locale. Esegui Pull e riprova.', 'pull'],
  Conflict: ['Conflitti', "L'operazione ha prodotto conflitti da risolvere.", null],
  DirtyWorktree: ['Modifiche locali', 'Le modifiche locali verrebbero sovrascritte: fai commit o stash.', null],
  Locked: ['Repository bloccato', 'Un altro processo git sta usando il repository (index.lock).', 'removeLock'],
  ClipboardUnavailable: ['Appunti non disponibili', 'Impossibile copiare negli appunti.', null],
  // Informational: shown after a successful lock removal.
  LockRemoved: ['Lock rimosso: riprova l’operazione', 'index.lock è stato rimosso.', null],
  LockInUse: ['Lock in uso', 'Un processo sta ancora usando index.lock: attendi la fine dell’operazione.', null],
  LockTooRecent: ['Lock appena creato', 'index.lock è stato creato pochi secondi fa: un’operazione git potrebbe essere in corso. Riprova tra poco.', null],
  LsofMissing: ['Verifica non possibile', 'lsof non è disponibile: non posso verificare che index.lock non sia in uso, quindi non lo rimuovo.', null],
  GitRunning: ['Git in esecuzione', 'Un processo git sta lavorando in questo repository: attendi che finisca, poi riprova a rimuovere index.lock.', null],
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
  AutostashConflict: ['Conflitti dopo il pull', 'Il pull è riuscito, ma riapplicare le modifiche locali ha prodotto conflitti: risolvili nei file. Le modifiche restano anche nella lista degli stash: eliminalo quando non serve più.', null],
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
  DiffChanged: ['Diff cambiato', 'Il file è cambiato da quando è stato mostrato: il diff è stato aggiornato, riprova.', null],
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

export function severityOf(e: UIError): Severity {
  return severities[e.kind] ?? 'error';
}

// User-facing text for an error. `stderr` is git's raw (English) output, kept apart so the
// UI can show it collapsed; an unknown kind falls back to the raw message.
export function describeError(e: UIError): { title: string; detail: string; stderr: string; action: ErrorAction; severity: Severity } {
  const t = texts[e.kind];
  const stderr = e.stderr.trim();
  const severity = severityOf(e);
  if (!t) return { title: 'Errore', detail: e.message || (stderr ? '' : 'Operazione non riuscita.'), stderr, action: null, severity };
  return { title: t[0], detail: t[1], stderr, action: t[2], severity };
}
