#!/usr/bin/env bash
# Embeds the bundled git and the askpass helper into the built .app and applies
# an ad-hoc signature (local use only, not notarized).
#
# Layout must match internal/gitexec/locate.go (paths relative to Contents/MacOS):
#   ../Resources/git/bin/git
#   ../Resources/askpass-helper
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

APP="build/bin/Gitrais.app" # wails.json productName
GIT_DIST="build/git-dist"
HELPER="build/bin/askpass-helper"
RES="$APP/Contents/Resources"

die() { echo "package.sh: $*" >&2; exit 1; }

[ -d "$APP" ] || die "$APP not found (run: make build)"
[ -x "$GIT_DIST/bin/git" ] || die "$GIT_DIST/bin/git missing (run: make git)"
[ -x "$HELPER" ] || die "$HELPER missing (run: make helper after make build; wails -clean wipes build/bin)"

# The distribution must be relocatable: every symlink has to be relative and
# stay inside git-dist, otherwise it would break or leak outside the bundle.
dist_abs="$(cd "$GIT_DIST" && pwd -P)"
while IFS= read -r -d '' link; do
  target="$(readlink "$link")"
  case "$target" in
    /*) die "absolute symlink in $GIT_DIST: $link -> $target" ;;
  esac
  resolved="$(cd "$(dirname "$link")" && cd "$(dirname "$target")" 2>/dev/null && pwd -P)/$(basename "$target")" \
    || die "dangling symlink in $GIT_DIST: $link -> $target"
  case "$resolved" in
    "$dist_abs"/*) ;;
    *) die "symlink escapes $GIT_DIST: $link -> $target" ;;
  esac
  [ -e "$link" ] || die "dangling symlink in $GIT_DIST: $link -> $target"
done < <(find "$GIT_DIST" -type l -print0)

# ditto preserves relative symlinks as symlinks; extended attributes and
# quarantine flags are not carried into the bundle.
rm -rf "$RES/git" "$RES/askpass-helper"
mkdir -p "$RES"
ditto --noextattr --noqtn "$GIT_DIST" "$RES/git"
ditto --noextattr --noqtn "$HELPER" "$RES/askpass-helper"

# Sign inside-out: every nested Mach-O first (regular files only; symlinks
# resolve to files signed here), then the app bundle itself.
signed=0
while IFS= read -r -d '' f; do
  if file -b "$f" | grep -q 'Mach-O'; then
    codesign --force --sign - "$f"
    signed=$((signed + 1))
  fi
done < <(find "$RES/git" -type f -print0)
codesign --force --sign - "$RES/askpass-helper"
codesign --force --sign - "$APP"
echo "signed $((signed + 1)) nested binaries plus the app"

codesign --verify --deep --strict "$APP"
"$RES/git/bin/git" --version
echo "Package ready: $APP"
