// Package ops implements the Core's git write operations on top of gitexec.
// All functions are stateless; serialization per repository is the caller's
// responsibility.
package ops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitrais/internal/gitexec"
)

// literalPaths makes git treat caller-supplied paths literally (no pathspec
// magic such as ":(glob)" or wildcards).
var literalPaths = []string{"GIT_LITERAL_PATHSPECS=1"}

func git(ctx context.Context, r *gitexec.Runner, dir string, env []string, args ...string) ([]byte, error) {
	return r.Run(ctx, gitexec.Cmd{Dir: dir, Args: args, Env: env})
}

func withPaths(base []string, paths []string) []string {
	return append(append([]string{}, base...), paths...)
}

func hasHead(ctx context.Context, r *gitexec.Runner, dir string) (bool, error) {
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "-q", "--verify", "HEAD"}, Env: gitexec.ReadOnlyEnv})
	if err == nil {
		return true, nil
	}
	if gitexec.ExitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

// StageFiles stages additions, modifications and deletions of the given paths.
func StageFiles(ctx context.Context, r *gitexec.Runner, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := git(ctx, r, dir, literalPaths, withPaths([]string{"add", "-A", "--"}, paths)...)
	return err
}

// StageAll stages every change in the working tree.
func StageAll(ctx context.Context, r *gitexec.Runner, dir string) error {
	_, err := git(ctx, r, dir, nil, "add", "-A")
	return err
}

// UnstageFiles resets the index entries of the given paths to HEAD (or removes
// them from the index when the branch is unborn).
func UnstageFiles(ctx context.Context, r *gitexec.Runner, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	ok, err := hasHead(ctx, r, dir)
	if err != nil {
		return err
	}
	if !ok {
		_, err = git(ctx, r, dir, literalPaths, withPaths([]string{"rm", "--cached", "-r", "-q", "--ignore-unmatch", "--"}, paths)...)
		return err
	}
	_, err = git(ctx, r, dir, literalPaths, withPaths([]string{"restore", "--staged", "--"}, paths)...)
	return err
}

// UnstageAll resets the whole index to HEAD (empties it when unborn).
func UnstageAll(ctx context.Context, r *gitexec.Runner, dir string) error {
	ok, err := hasHead(ctx, r, dir)
	if err != nil {
		return err
	}
	if !ok {
		_, err = git(ctx, r, dir, nil, "rm", "--cached", "-r", "-q", "--ignore-unmatch", "--", ".")
		return err
	}
	_, err = git(ctx, r, dir, nil, "restore", "--staged", "--", ":/")
	return err
}

// ApplyPatch applies a patch to the index only (always --cached). With
// reverse the patch is applied backwards (used to unstage).
func ApplyPatch(ctx context.Context, r *gitexec.Runner, dir string, patch []byte, reverse bool) error {
	args := []string{"apply", "--cached", "--recount", "--whitespace=nowarn"}
	if reverse {
		args = append(args, "--reverse")
	}
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: append(args, "-"), Stdin: bytes.NewReader(patch)})
	return err
}

// DiscardFiles restores tracked files from the index and deletes untracked
// ones. Untracked paths must stay inside the repository (symlinks included)
// and may not touch .git; nothing is changed if any path is rejected.
func DiscardFiles(ctx context.Context, r *gitexec.Runner, dir string, tracked, untracked []string) error {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	inside := func(p string) bool { return p != root && strings.HasPrefix(p, root+string(filepath.Separator)) }
	var abs []string
	for _, p := range untracked {
		a := filepath.Join(root, p)
		if !inside(a) {
			return fmt.Errorf("path outside the repository: %s", p)
		}
		if rel, _ := filepath.Rel(root, a); rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			return fmt.Errorf("refusing to delete inside .git: %s", p)
		}
		// Resolve symlinked parent directories so a link can't lead outside.
		parent, err := filepath.EvalSymlinks(filepath.Dir(a))
		if err != nil {
			if os.IsNotExist(err) {
				continue // already gone
			}
			return err
		}
		if parent != root && !inside(parent) {
			return fmt.Errorf("path outside the repository: %s", p)
		}
		abs = append(abs, a)
	}
	if len(tracked) > 0 {
		if _, err := git(ctx, r, dir, literalPaths, withPaths([]string{"restore", "--worktree", "--"}, tracked)...); err != nil {
			return err
		}
	}
	for _, a := range abs {
		if err := os.RemoveAll(a); err != nil {
			return err
		}
	}
	return nil
}
