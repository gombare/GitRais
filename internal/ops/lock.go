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
)

// minLockAge is how old a lock must be before it may be removed.
const minLockAge = 5 * time.Second

// lsofTimeout bounds the lsof call; on expiry the lock is left in place.
// A variable so tests can shorten it.
var lsofTimeout = 10 * time.Second

// lsofPath is a variable so tests can simulate a missing lsof.
var lsofPath = "/usr/sbin/lsof"

// RemoveIndexLock deletes <gitDir>/index.lock, and nothing else, once it is
// safe: the lock must be a regular file, at least minLockAge old, and no
// process may hold it open (checked with lsof). A missing lock is a no-op.
// gitDir must come from the session, never from the frontend.
func RemoveIndexLock(ctx context.Context, gitDir string) error {
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
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, lsofPath, "-t", "--", p)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && ctx.Err() != nil {
		// Timed out or cancelled: a killed lsof looks like "no holder", so check first.
		return ctx.Err()
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee) && ee.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0 && stderr.Len() == 0:
		// lsof exits 1 without output when no process has the file open.
	case errors.As(err, &ee) && ee.ExitCode() == 1 && len(bytes.TrimSpace(out)) > 0:
	default:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("lsof failed: %w", err)
	}
	if len(bytes.TrimSpace(out)) > 0 {
		return ErrLockInUse
	}
	// Residual race: a git process may create or open the lock between the
	// lsof check and the removal. It cannot be closed without kernel support;
	// the age check and the user's confirmation keep the window small.
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
