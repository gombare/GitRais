// Ahead/behind badge: an arrow and a count; screen readers get the spelled-out text instead.
export function AheadBehind({ ahead, behind }: { ahead: number; behind: number }) {
  return (
    <>
      {ahead > 0 && <Badge kind="ahead" short={`↑${ahead}`} long={`${ahead} commit da inviare`} />}
      {behind > 0 && <Badge kind="behind" short={`↓${behind}`} long={`${behind} commit da ricevere`} />}
    </>
  );
}

export function Badge({ kind, short, long }: { kind: 'ahead' | 'behind'; short: string; long: string }) {
  return (
    <span className={`badge badge-${kind}`} title={long}>
      <span aria-hidden="true">{short}</span><span className="sr-only">{long}</span>
    </span>
  );
}
