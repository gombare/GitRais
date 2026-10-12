#!/usr/bin/env bash
# Compila git da sorgente in una installazione rilocabile (RUNTIME_PREFIX).
set -euo pipefail
VERSION="$1"; DEST="$(cd "$(dirname "$2")" && pwd)/$(basename "$2")"
SRC="build/git-src"
mkdir -p "$SRC"
if [ ! -d "$SRC/git-$VERSION" ]; then
  curl -fsSL "https://mirrors.edge.kernel.org/pub/software/scm/git/git-$VERSION.tar.xz" -o "$SRC/git.tar.xz"
  tar -xJf "$SRC/git.tar.xz" -C "$SRC"
fi
cd "$SRC/git-$VERSION"
MAKEFLAGS=(prefix=/ RUNTIME_PREFIX=YesPlease gitexecdir=libexec/git-core
  NO_PERL=YesPlease NO_TCLTK=YesPlease NO_GETTEXT=YesPlease NO_PYTHON=YesPlease
  NO_DARWIN_PORTS=YesPlease NO_FINK=YesPlease NO_INSTALL_HARDLINKS=YesPlease
  CFLAGS="-O2 -mmacosx-version-min=12.0")
make -j"$(sysctl -n hw.ncpu)" "${MAKEFLAGS[@]}" all
rm -rf "$DEST"
make "${MAKEFLAGS[@]}" DESTDIR="$DEST" strip install
make -C contrib/credential/osxkeychain CFLAGS="-O2 -mmacosx-version-min=12.0"
cp contrib/credential/osxkeychain/git-credential-osxkeychain "$DEST/libexec/git-core/"
mkdir -p "$DEST/etc"
printf '[credential]\n\thelper = osxkeychain\n' > "$DEST/etc/gitconfig"
"$DEST/bin/git" --version
