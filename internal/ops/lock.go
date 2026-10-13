package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	// ErrLockInUse is returned by RemoveIndexLock when a process holds the lock open.
	ErrLockInUse = errors.New("index.lock is held by a running process")
	// ErrLockTooRecent is returned by RemoveIndexLock for a lock younger than
	// minLockAge: a git command may be starting right now.
	ErrLockTooRecent = errors.New("index.lock was just created: wait a few seconds and retry")
	// ErrLsofMissing is returned by RemoveIndexLock when lsof is unavailable, so
	// nobody can tell whether the lock is held; it is never deleted blindly.
	ErrLsofMissing = errors.New("lsof is not available: cannot verify that index.lock is unused")
	// ErrGitRunning is returned by RemoveIndexLock when a git process works
	// inside the repository: it may own the lock even without holding it open.
	ErrGitRunning = errors.New("a git process is running in this repository")
	// errLsofFailed wraps an lsof failure; the lock is then kept.
	errLsofFailed = errors.New("lsof failed")
)

// minLockAge is how old a lock must be before it may be removed.
const minLockAge = 5 * time.Second

// lsofTimeout bounds the lsof call; on expiry the lock is left in place.
// A variable so tests can shorten it.
var lsofTimeout = 10 * time.Second

// lsofPath is a variable so tests can simulate a missing lsof.
var lsofPath = "/usr/sbin/lsof"

// RemoveIndexLock deletes <gitDir>/index.lock, and nothing else, once it is
// safe: the lock must be a regular file, at least minLockAge old, no process
// may hold it open and no git process may have its working directory inside
// gitDir or worktree (both checked with lsof, failing closed). A missing lock
// is a no-op. gitDir and worktree must come from the session, never from the
// frontend.
func RemoveIndexLock(ctx context.Context, gitDir, worktree string) error {
	if gitDir == "" || !filepath.IsAbs(gitDir) {
		return fmt.Errorf("invalid git directory %q", gitDir)
	}
	p := filepath.Join(filepath.Clean(gitDir), "index.lock")
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", p)
	}
	if time.Since(fi.ModTime()) < minLockAge {
		return ErrLockTooRecent
	}
	if _, err := os.Stat(lsofPath); err != nil {
		return ErrLsofMissing
	}
	// lsof can hang on a stuck network mount: bound it and fail closed.
	ctx, cancel := context.WithTimeout(ctx, lsofTimeout)
	defer cancel()
	holders, err := runLsof(ctx, "-t", "--", p)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(holders)) > 0 {
		return ErrLockInUse
	}
	// A git process working in the repository may own the lock without
	// holding it open (between creating it and renaming it over the index).
	cwds, err := runLsof(ctx, "-a", "-c", "git", "-d", "cwd", "-Fn")
	if err != nil {
		return err
	}
	roots := []string{gitDir}
	if worktree != "" {
		roots = append(roots, worktree)
	}
	if gitCwdWithin(cwds, roots) {
		return ErrGitRunning
	}
	// Residual race: a git process may create or open the lock between the
	// lsof check and the removal. It cannot be closed without kernel support;
	// the age check and the user's confirmation keep the window small.
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// runLsof runs lsof with args. Exit status 1 without stderr means "nothing
// matched" and returns the (normally empty) output; anything else that is not
// a success is an error, so callers fail closed.
func runLsof(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, lsofPath, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && ctx.Err() != nil {
		// Timed out or cancelled: a killed lsof looks like "no match", so check first.
		return nil, ctx.Err()
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee) && ee.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0 && stderr.Len() == 0:
		// lsof exits 1 without output when nothing matches.
	case errors.As(err, &ee) && ee.ExitCode() == 1 && len(bytes.TrimSpace(out)) > 0:
		// Some matches, plus files it could not stat: the output still counts.
	default:
		return nil, fmt.Errorf("%w: %v %s", errLsofFailed, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return out, nil
}

// gitCwdWithin reports whether any "n<path>" line of `lsof -Fn` output names a
// directory equal to or inside one of roots. Paths are compared after
// resolving symlinks, and case-insensitively on darwin (APFS default).
func gitCwdWithin(lsofOut []byte, roots []string) bool {
	norm := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		p = filepath.Clean(p)
		if runtime.GOOS == "darwin" {
			p = strings.ToLower(p)
		}
		return p
	}
	rs := make([]string, 0, len(roots))
	for _, r := range roots {
		rs = append(rs, norm(r))
	}
	for _, line := range strings.Split(string(lsofOut), "\n") {
		p, ok := strings.CutPrefix(line, "n")
		if !ok || !filepath.IsAbs(p) {
			continue
		}
		p = norm(p)
		for _, r := range rs {
			if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}
