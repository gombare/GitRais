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
// them from the index when the branch is unborn; with --cached, -f only skips
// the "staged content differs" safety check and never touches the worktree).
func UnstageFiles(ctx context.Context, r *gitexec.Runner, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	ok, err := hasHead(ctx, r, dir)
	if err != nil {
		return err
	}
	if !ok {
		_, err = git(ctx, r, dir, literalPaths, withPaths([]string{"rm", "--cached", "-f", "-r", "-q", "--ignore-unmatch", "--"}, paths)...)
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
		_, err = git(ctx, r, dir, nil, "rm", "--cached", "-f", "-r", "-q", "--ignore-unmatch", "--", ".")
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

// isGitName reports whether a path component names a .git directory in any
// letter case (case-insensitive file systems resolve ".GIT" to ".git").
func isGitName(c string) bool { return strings.EqualFold(c, ".git") }

// inGitDir reports whether p (below root) is the git directory or lies inside
// it. Each ancestor up to root is compared with os.SameFile, so differences in
// case or Unicode normalization on case-insensitive file systems can't hide a
// match.
func inGitDir(p, root string, gitDir os.FileInfo) bool {
	for cur := p; cur != root; {
		if fi, err := os.Stat(cur); err == nil && os.SameFile(fi, gitDir) {
			return true
		}
		next := filepath.Dir(cur)
		if next == cur {
			return false
		}
		cur = next
	}
	return false
}

// DiscardFiles restores tracked files from the index and deletes untracked
// ones. Untracked paths must stay inside the repository (symlinks included)
// and may not touch the git directory or any .git component (nested
// repositories); nothing is changed if any path is rejected. Only files that
// are actually untracked and not ignored are removed (git clean), so tracked,
// staged or ignored files inside an untracked directory survive.
func DiscardFiles(ctx context.Context, r *gitexec.Runner, dir string, tracked, untracked []string) error {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	var gitDirInfo os.FileInfo
	if len(untracked) > 0 {
		out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "--absolute-git-dir"}, Env: gitexec.ReadOnlyEnv})
		if err != nil {
			return err
		}
		if gitDirInfo, err = os.Stat(strings.TrimRight(string(out), "\r\n")); err != nil {
			return err
		}
	}
	inside := func(p string) bool { return p != root && strings.HasPrefix(p, root+string(filepath.Separator)) }
	var rels []string
	for _, p := range untracked {
		a := filepath.Join(root, p)
		if !inside(a) {
			return fmt.Errorf("path outside the repository: %s", p)
		}
		rel, _ := filepath.Rel(root, a)
		for _, c := range strings.Split(filepath.ToSlash(rel), "/") {
			if isGitName(c) {
				return fmt.Errorf("refusing to delete inside .git: %s", p)
			}
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
		if inGitDir(filepath.Join(parent, filepath.Base(a)), root, gitDirInfo) {
			return fmt.Errorf("refusing to delete inside .git: %s", p)
		}
		rels = append(rels, filepath.ToSlash(rel))
	}
	if len(tracked) > 0 {
		if _, err := git(ctx, r, dir, literalPaths, withPaths([]string{"restore", "--worktree", "--"}, tracked)...); err != nil {
			return err
		}
	}
	if len(rels) > 0 {
		// -d removes untracked directories, but git clean never descends into
		// nested repositories without a second -f, nor removes ignored files.
		if _, err := git(ctx, r, root, literalPaths, withPaths([]string{"clean", "-f", "-d", "-q", "--"}, rels)...); err != nil {
			return err
		}
	}
	return nil
}
