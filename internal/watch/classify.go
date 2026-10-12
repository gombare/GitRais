// Package watch observes a repository and reports changes to the working tree (WIP)
// or to refs.
package watch

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Kind is a bit set of refresh kinds.
type Kind int

const (
	// WIP means the working tree or the index changed.
	WIP Kind = 1 << iota
	// Refs means branches, tags, HEAD or other ref state changed.
	Refs
)

// caseInsensitive makes path-prefix matching ignore case (default macOS/Windows
// file systems). A variable so tests can exercise both modes.
var caseInsensitive = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// Classify maps a changed absolute path to the refresh kinds it requires.
//
// root and gitDir should be real paths (filepath.EvalSymlinks). gitDir may live
// outside root (linked worktrees, separate git dirs). ignore, when non-nil, is
// asked about worktree-relative slash paths only (never for paths in gitDir).
//
// FSEvents can coalesce changes into a directory path: a directory inside the
// worktree (or root itself) counts as WIP, and a directory under refs/ as Refs.
func Classify(root, gitDir, path string, ignore func(rel string) bool) Kind {
	if rel, ok := under(gitDir, path); ok {
		if strings.HasSuffix(rel, ".lock") {
			return 0
		}
		switch {
		case rel == "index":
			return WIP
		case rel == "HEAD":
			return Refs | WIP
		case rel == "packed-refs", rel == "FETCH_HEAD", rel == "ORIG_HEAD", rel == "MERGE_HEAD",
			rel == "CHERRY_PICK_HEAD", rel == "REVERT_HEAD", rel == "REBASE_HEAD",
			rel == "refs", strings.HasPrefix(rel, "refs/"):
			return Refs
		}
		// objects/**, logs/**, hooks, worktrees/**, ...: noise.
		return 0
	}
	if rel, ok := under(root, path); ok {
		// With a gitDir outside root (linked worktree) root/.git is a plain file.
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			return 0
		}
		if rel == "." {
			return WIP // coalesced event on the worktree root itself
		}
		if ignore != nil && ignore(rel) {
			return 0
		}
		return WIP
	}
	return 0
}

// under returns p relative to base (slash separated, "." when equal) and whether
// p is base or inside it.
func under(base, p string) (string, bool) {
	base, p = filepath.Clean(base), filepath.Clean(p)
	if rel, ok := relUnder(base, p); ok {
		return rel, true
	}
	if caseInsensitive && len(p) >= len(base) && strings.EqualFold(p[:len(base)], base) {
		return relUnder(p[:len(base)], p)
	}
	return "", false
}

func relUnder(base, p string) (string, bool) {
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
