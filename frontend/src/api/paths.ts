// Display-only basename for plain paths coming from persisted state (recents/favorites).
// Handles both "/" and "\\" separators. Never use it to build or validate paths.
export function displayName(p: string): string {
  const parts = p.split(/[/\\]+/).filter(Boolean);
  return parts.length ? parts[parts.length - 1] : p;
}
