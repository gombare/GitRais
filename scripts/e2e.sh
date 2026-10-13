#!/usr/bin/env bash
# Builds a throwaway repository with a local bare remote, starts `wails dev` with isolated
# state/log paths and runs the Playwright tests against its browser endpoint.
# Never touches ~/Library/Application Support/Gitrais or ~/Library/Logs/Gitrais.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GIT="${GITRAIS_GIT:-$ROOT/build/git-dist/bin/git}"
WAILS="$(go env GOPATH)/bin/wails"
URL="http://localhost:34115"
TIMEOUT="${E2E_START_TIMEOUT:-180}"

[ -x "$GIT" ] || { echo "bundled git not found at $GIT (run make git)" >&2; exit 1; }
[ -x "$WAILS" ] || { echo "wails not found at $WAILS" >&2; exit 1; }
if curl -sf -o /dev/null "$URL"; then
  echo "something already listens on $URL: stop it first (it could be using the real state)" >&2
  exit 1
fi

# Pre-flight before booting wails: dependencies and the Playwright browser.
if [ ! -d "$ROOT/frontend/node_modules" ]; then
  echo "frontend/node_modules missing: run 'cd frontend && npm install'" >&2
  exit 1
fi
CHROMIUM="$(cd "$ROOT/frontend" && node -e "process.stdout.write(require('@playwright/test').chromium.executablePath())" 2>/dev/null || true)"
if [ -z "$CHROMIUM" ] || [ ! -e "$CHROMIUM" ]; then
  echo "Playwright chromium missing: run 'cd frontend && npx playwright install chromium'" >&2
  exit 1
fi

WORK="$(mktemp -d)"
WAILS_PID=""
cleanup() {
  if [ -n "$WAILS_PID" ]; then
    # wails dev spawns vite and the app: terminate its whole process group.
    kill -TERM -- "-$WAILS_PID" 2>/dev/null || kill -TERM "$WAILS_PID" 2>/dev/null || true
    sleep 1
    kill -KILL -- "-$WAILS_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

export GITRAIS_GIT="$GIT"
export GITRAIS_STATE_PATH="$WORK/state.json" GITRAIS_LOG_PATH="$WORK/activity.log"
export GITRAIS_E2E_GIT="$GIT" GITRAIS_E2E_BARE="$WORK/remote.git" GITRAIS_E2E_REPO="$WORK/repo"

"$GIT" init -q --bare -b main "$GITRAIS_E2E_BARE"
"$GIT" init -q -b main "$GITRAIS_E2E_REPO"
"$GIT" -C "$GITRAIS_E2E_REPO" config user.name E2E
"$GIT" -C "$GITRAIS_E2E_REPO" config user.email e2e@example.com
"$GIT" -C "$GITRAIS_E2E_REPO" config commit.gpgsign false
seq -f 'line %g' 1 30 > "$GITRAIS_E2E_REPO/multi.txt"
"$GIT" -C "$GITRAIS_E2E_REPO" add multi.txt
"$GIT" -C "$GITRAIS_E2E_REPO" commit -q -m initial
"$GIT" -C "$GITRAIS_E2E_REPO" remote add origin "$GITRAIS_E2E_BARE"

cd "$ROOT"
make helper
# Own process group (job control) so cleanup reaches vite and the app too.
set -m
"$WAILS" dev -nogorebuild -skipembedcreate > "$WORK/wails.log" 2>&1 &
WAILS_PID=$!
set +m

for _ in $(seq 1 "$TIMEOUT"); do
  curl -sf -o /dev/null "$URL" && break
  kill -0 "$WAILS_PID" 2>/dev/null || { echo "wails dev exited early:" >&2; tail -30 "$WORK/wails.log" >&2; exit 1; }
  sleep 1
done
if ! curl -sf -o /dev/null "$URL"; then
  echo "wails dev did not answer on $URL within ${TIMEOUT}s:" >&2
  tail -30 "$WORK/wails.log" >&2
  exit 1
fi

cd frontend
npx playwright test "$@"
