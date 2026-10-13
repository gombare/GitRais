#!/usr/bin/env bash
# Usage: parity-trace.sh start <name> | stop <name>
# Records every git process started by any program (the reference client included) via trace2.
# start: backs up ~/.gitconfig and sets trace2.normalTarget globally.
# stop:  unsets it (the backup stays in $OUT) and extracts the command lines.
set -euo pipefail
OUT="$HOME/gitrais-parity"
mkdir -p "$OUT"
name="${2:?usage: $0 start|stop <name>}"
case "$1" in
  start)
    if git config --global --get trace2.normalTarget >/dev/null; then
      echo "trace2.normalTarget is already set: run '$0 stop <name>' first" >&2
      exit 1
    fi
    cp "$HOME/.gitconfig" "$OUT/gitconfig.backup" 2>/dev/null || true
    git config --global trace2.normalTarget "$OUT/$name.trace"
    echo "Tracing to $OUT/$name.trace - run the flow, then: $0 stop $name" ;;
  stop)
    git config --global --unset trace2.normalTarget || true
    touch "$OUT/$name.trace"
    grep -E ' start ' "$OUT/$name.trace" | sed -E 's/.* start [^ ]*git(-[a-z-]+)? ?/git /' > "$OUT/$name.commands" || true
    echo "Commands in $OUT/$name.commands" ;;
  *) echo "usage: $0 start|stop <name>" >&2; exit 2 ;;
esac
