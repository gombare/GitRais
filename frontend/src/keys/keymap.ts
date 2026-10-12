// Command names follow the usual Git GUI scheme "<area>:<action>". The default
// accelerators are Gitrais' own choice (macOS conventions: Cmd).
export type Command = 'repo:fetch' | 'repo:stageAll' | 'repo:unstageAll' | 'repo:createBranch' | 'commit:commit'
  | 'commit:commitAll' | 'select:next' | 'select:previous' | 'view:toggleDetailPanel' | 'view:close'
  | 'view:reopenTab' | 'workDir:focusCommitMessage';

export const defaultKeymap: Record<Command, string> = {
  'repo:fetch': 'Cmd+Shift+F',
  'repo:stageAll': 'Cmd+Shift+S',
  'repo:unstageAll': 'Cmd+Shift+U',
  'repo:createBranch': 'Cmd+B',
  'commit:commit': 'Cmd+Enter',
  'commit:commitAll': 'Cmd+Shift+Enter',
  'select:next': 'ArrowDown',
  'select:previous': 'ArrowUp',
  'view:toggleDetailPanel': 'Cmd+J',
  'view:close': 'Cmd+W',
  'view:reopenTab': 'Cmd+Shift+T',
  'workDir:focusCommitMessage': 'Cmd+Shift+M',
};

export type KeyLike = { key: string; metaKey: boolean; shiftKey: boolean; altKey: boolean; ctrlKey: boolean };

export function matches(e: KeyLike, accel: string): boolean {
  const parts = accel.split('+');
  const key = parts.pop()!;
  const want = { Cmd: false, Shift: false, Alt: false, Ctrl: false };
  for (const p of parts) want[p as keyof typeof want] = true;
  return e.metaKey === want.Cmd && e.shiftKey === want.Shift && e.altKey === want.Alt && e.ctrlKey === want.Ctrl
    && e.key.toLowerCase() === key.toLowerCase();
}

export function commandFor(e: KeyLike, keymap: Record<Command, string> = defaultKeymap): Command | null {
  for (const [cmd, accel] of Object.entries(keymap) as [Command, string][]) {
    if (matches(e, accel)) return cmd;
  }
  return null;
}
