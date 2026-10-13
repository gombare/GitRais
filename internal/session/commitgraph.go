package session

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"gitrais/internal/gitexec"
)

// Test seams.
var (
	// autoCommitGraph enables the background commit-graph write at Open.
	autoCommitGraph = true
	// writeCommitGraph writes the commit-graph of the repository at dir.
	writeCommitGraph = runCommitGraphWrite
)

// runCommitGraphWrite writes a split commit-graph of every reachable commit.
// It is a write (no GIT_OPTIONAL_LOCKS=0) but touches only objects/info.
func runCommitGraphWrite(ctx context.Context, r *gitexec.Runner, dir string) error {
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"commit-graph", "write", "--reachable", "--split", "--no-progress"}})
	return err
}

// needsCommitGraph reports whether the repository whose common git dir is
// commonDir has no commit-graph yet and its object store is writable. Without
// a commit-graph `git log --topo-order` must parse every commit before
// printing the first one, which takes seconds on large fresh clones.
func needsCommitGraph(commonDir string) bool {
	info := filepath.Join(commonDir, "objects", "info")
	for _, p := range []string{"commit-graph", "commit-graphs"} {
		if _, err := os.Stat(filepath.Join(info, p)); !errors.Is(err, fs.ErrNotExist) {
			return false // present, or unknown: leave it alone
		}
	}
	dir := info
	if _, err := os.Stat(info); errors.Is(err, fs.ErrNotExist) {
		dir = filepath.Join(commonDir, "objects")
	}
	return syscall.Access(dir, 2 /* W_OK */) == nil
}

// startCommitGraph writes the commit-graph in the background when the
// repository has none, then reloads the graph (now fast). It never blocks the
// graph load, runs at most once at a time per session and is cancelled by
// Close, which waits for it (it counts as a loader).
func (s *Session) startCommitGraph() {
	if !autoCommitGraph || !needsCommitGraph(s.commonDir) || !s.cgRunning.CompareAndSwap(false, true) {
		return
	}
	s.loadMu.Lock()
	if s.loadClosed {
		s.loadMu.Unlock()
		s.cgRunning.Store(false)
		return
	}
	s.loadWG.Add(1)
	s.loadMu.Unlock()
	go func() {
		defer s.loadWG.Done()
		defer s.cgRunning.Store(false)
		// A failure (read-only store, shallow clone, ...) only keeps the slow path.
		if err := writeCommitGraph(s.ctx, s.r, s.Dir); err == nil && s.ctx.Err() == nil {
			s.Reload()
		}
	}()
}
