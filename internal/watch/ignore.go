package watch

import (
	"context"
	"strings"
	"sync"

	"gitrais/internal/gitexec"
)

// IgnoredPaths returns a predicate over worktree-relative slash paths that are
// ignored by gitignore rules (.gitignore files, info/exclude, core.excludesFile).
//
// Cost model: one `git ls-files --others --ignored --exclude-standard --directory`
// at creation (it walks the worktree once; wholly ignored directories such as
// node_modules are reported as a single entry and not descended into, so the cost
// is roughly that of `git status` on untracked files). The predicate itself is an
// in-memory map lookup: no git process is spawned per event.
//
// The snapshot is rebuilt (one more git run, synchronously, from the caller of the
// predicate) when the predicate is asked about a ".gitignore" file, i.e. when a
// .gitignore changes. If the rebuild fails the previous snapshot is kept. Limits:
// a new ignored file that was not in the snapshot (for instance a fresh x.log
// under a pattern, or an edit to .git/info/exclude) is reported as not ignored
// until the next rebuild, which only costs a spurious WIP refresh. ctx is reused
// for rebuilds, so it should live as long as the watcher.
func IgnoredPaths(ctx context.Context, r *gitexec.Runner, dir string) (func(rel string) bool, error) {
	snap, err := loadIgnored(ctx, r, dir)
	if err != nil {
		return nil, err
	}
	var mu sync.RWMutex
	var refresh sync.Mutex
	return func(rel string) bool {
		rel = strings.TrimSuffix(rel, "/")
		if rel == ".gitignore" || strings.HasSuffix(rel, "/.gitignore") {
			refresh.Lock()
			if next, err := loadIgnored(ctx, r, dir); err == nil {
				mu.Lock()
				snap = next
				mu.Unlock()
			}
			refresh.Unlock()
			return false
		}
		mu.RLock()
		s := snap
		mu.RUnlock()
		return s.match(rel)
	}, nil
}

type ignoredSnapshot struct {
	files map[string]bool // ignored files
	dirs  map[string]bool // wholly ignored directories, without trailing slash
}

func loadIgnored(ctx context.Context, r *gitexec.Runner, dir string) (*ignoredSnapshot, error) {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Env: gitexec.ReadOnlyEnv,
		Args: []string{"ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z"}})
	if err != nil {
		return nil, err
	}
	s := &ignoredSnapshot{files: map[string]bool{}, dirs: map[string]bool{}}
	for _, p := range strings.Split(string(out), "\x00") {
		switch {
		case p == "":
		case strings.HasSuffix(p, "/"):
			s.dirs[strings.TrimSuffix(p, "/")] = true
		default:
			s.files[p] = true
		}
	}
	return s, nil
}

// match reports whether rel, or any ancestor directory of it, is ignored.
func (s *ignoredSnapshot) match(rel string) bool {
	if s.files[rel] || s.dirs[rel] {
		return true
	}
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' && s.dirs[rel[:i]] {
			return true
		}
	}
	return false
}
