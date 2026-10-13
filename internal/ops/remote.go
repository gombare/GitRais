package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitrais/internal/gitexec"
)

var (
	// ErrPullStrategyRequired is returned by Pull with PullDefault when the
	// branches have diverged and pull.rebase is not configured: git refuses to
	// guess, and the caller must retry with an explicit PullMode.
	ErrPullStrategyRequired = errors.New("branches have diverged: choose merge, rebase or fast-forward only")
	// ErrDestNotEmpty is returned by Clone when the destination exists and is
	// not an empty directory.
	ErrDestNotEmpty = errors.New("destination exists and is not an empty directory")
	// ErrAutostashConflict is returned by PullAutostash when the pull itself
	// succeeded but re-applying the stashed local changes conflicted: git kept
	// the stash entry (it is never dropped) and left conflict markers in the
	// working tree.
	ErrAutostashConflict = errors.New("pulled, but re-applying the local changes conflicted: they are kept in the stash list")
)

// OnProgress receives progress updates parsed from git's stderr.
type OnProgress func(gitexec.Progress)

// withProgress adapts p to a Cmd.OnStderr callback; it is nil-safe.
func withProgress(p OnProgress) func([]byte) {
	if p == nil {
		return nil
	}
	pp := &gitexec.ProgressParser{On: p}
	return pp.Write
}

// noEditorEnv guarantees git never opens an editor (merge message, rebase todo).
var noEditorEnv = []string{"GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "GIT_MERGE_AUTOEDIT=no"}

// Fetch fetches all remotes and prunes stale remote-tracking refs.
func Fetch(ctx context.Context, r *gitexec.Runner, dir string, p OnProgress) error {
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"fetch", "--all", "--prune", "--progress"}, OnStderr: withProgress(p)})
	return err
}

// PullMode selects how Pull reconciles the local branch with its upstream.
type PullMode string

const (
	PullDefault PullMode = ""
	PullMerge   PullMode = "merge"
	PullRebase  PullMode = "rebase"
	PullFFOnly  PullMode = "ff-only"
)

// Pull fetches and integrates the upstream of the current branch. It never
// prompts nor opens an editor. With PullDefault and diverged branches (and no
// pull.rebase configured) it returns ErrPullStrategyRequired. A merge or
// rebase that stops on conflicts returns an error of KindConflict (decided by
// unmerged index entries) and deliberately leaves the repository in the
// in-progress state so the UI can offer continue/abort. Local changes that the
// pull would overwrite make it fail with KindDirtyWorktree (see PullAutostash).
func Pull(ctx context.Context, r *gitexec.Runner, dir string, mode PullMode, p OnProgress) error {
	return pull(ctx, r, dir, mode, false, p)
}

// PullAutostash is Pull with `--autostash`: local changes are stashed before
// the integration and re-applied after it. When re-applying conflicts, git
// keeps the stash entry and leaves conflict markers; PullAutostash then
// returns ErrAutostashConflict (detected by refs/stash moving, i.e. a stash
// entry left behind). It never drops a stash.
func PullAutostash(ctx context.Context, r *gitexec.Runner, dir string, mode PullMode, p OnProgress) error {
	return pull(ctx, r, dir, mode, true, p)
}

// stashTop returns the commit refs/stash points to ("" when there is none).
func stashTop(ctx context.Context, r *gitexec.Runner, dir string) (string, error) {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "-q", "--verify", "refs/stash"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		if gitexec.ExitCode(err) == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func pull(ctx context.Context, r *gitexec.Runner, dir string, mode PullMode, autostash bool, p OnProgress) error {
	args := []string{"pull", "--progress", "--no-edit"}
	switch mode {
	case PullDefault:
	case PullMerge:
		args = append(args, "--no-rebase")
	case PullRebase:
		args = append(args, "--rebase")
	case PullFFOnly:
		args = append(args, "--ff-only")
	default:
		return fmt.Errorf("invalid pull mode: %q", mode)
	}
	before := ""
	if autostash {
		args = append(args, "--autostash")
		var err error
		if before, err = stashTop(ctx, r, dir); err != nil {
			return err
		}
	}
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: args, Env: noEditorEnv, OnStderr: withProgress(p)})
	if err == nil {
		if autostash {
			// git exits 0 when only the re-application of the autostash
			// conflicted: the entry it kept is the only trace.
			after, serr := stashTop(ctx, r, dir)
			if serr != nil {
				return serr
			}
			if after != before {
				return ErrAutostashConflict
			}
		}
		return nil
	}
	var ge *gitexec.GitError
	if !errors.As(err, &ge) {
		return err
	}
	if strings.Contains(ge.Stderr, "Need to specify how to reconcile divergent branches") {
		return fmt.Errorf("%w: %w", ErrPullStrategyRequired, err)
	}
	// Conflict messages may go to stdout, so rely on unmerged index entries.
	u, uerr := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"ls-files", "-u", "-z"}, Env: gitexec.ReadOnlyEnv})
	if uerr == nil && len(u) > 0 {
		if autostash && strings.Contains(ge.Stderr, "Applying autostash resulted in conflicts") {
			return fmt.Errorf("%w: %w", ErrAutostashConflict, err)
		}
		ge.Kind = gitexec.KindConflict
	}
	return err
}

// PushOptions describes a push of one local branch.
type PushOptions struct {
	Remote         string `json:"remote"`
	Branch         string `json:"branch"`       // local branch
	RemoteBranch   string `json:"remoteBranch"` // remote branch; empty = same name as the local one
	SetUpstream    bool   `json:"setUpstream"`
	ForceWithLease bool   `json:"forceWithLease"`
}

// Push pushes refs/heads/<Branch> to refs/heads/<RemoteBranch> on Remote.
// ForceWithLease uses an explicit lease pinned to the current value of the
// remote-tracking ref (refs/remotes/<remote>/<remoteBranch>), never the bare
// --force-with-lease, which trusts whatever a background fetch last stored.
// When that tracking ref does not exist the lease expects the remote branch to
// be absent, so the push cannot overwrite a branch we never saw. Plain --force
// is never used.
func Push(ctx context.Context, r *gitexec.Runner, dir string, o PushOptions, p OnProgress) error {
	if err := validateRemoteName(o.Remote); err != nil {
		return err
	}
	if err := ValidateBranchName(ctx, r, dir, o.Branch); err != nil {
		return err
	}
	dst := o.RemoteBranch
	if dst == "" {
		dst = o.Branch
	}
	if err := ValidateBranchName(ctx, r, dir, dst); err != nil {
		return err
	}
	args := []string{"push", "--progress"}
	if o.SetUpstream {
		args = append(args, "--set-upstream")
	}
	if o.ForceWithLease {
		expected := ""
		out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "-q", "--verify", "refs/remotes/" + o.Remote + "/" + dst}, Env: gitexec.ReadOnlyEnv})
		if err == nil {
			expected = strings.TrimSpace(string(out))
		} else if gitexec.ExitCode(err) != 1 {
			return err
		}
		args = append(args, "--force-with-lease=refs/heads/"+dst+":"+expected)
	}
	args = append(args, "--end-of-options", o.Remote, "refs/heads/"+o.Branch+":refs/heads/"+dst)
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: args, OnStderr: withProgress(p)})
	return err
}

// Clone clones url into dest. dest must not exist or must be an empty
// directory (otherwise ErrDestNotEmpty) and must be absolute. If the clone
// fails, only what this call created is removed: a dest it created is deleted,
// a pre-existing empty directory is emptied but kept. Credentials embedded in url are redacted by
// gitexec in returned errors.
func Clone(ctx context.Context, r *gitexec.Runner, url, dest string, p OnProgress) error {
	if url == "" || strings.HasPrefix(url, "-") {
		return fmt.Errorf("invalid repository URL: %q", gitexec.RedactURL(url))
	}
	if dest == "" || strings.HasPrefix(dest, "-") {
		return fmt.Errorf("invalid destination: %q", dest)
	}
	if !filepath.IsAbs(dest) {
		return fmt.Errorf("destination must be an absolute path: %q", dest)
	}
	dest = filepath.Clean(dest)
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	// Claim dest atomically: only what this call creates is ever removed, and
	// a symlink at dest is never followed or deleted.
	created := false
	if err := os.Mkdir(dest, 0o755); err == nil {
		created = true
	} else if !os.IsExist(err) {
		return err
	} else {
		fi, lerr := os.Lstat(dest)
		if lerr != nil {
			return lerr
		}
		if !fi.IsDir() { // regular file, symlink (even dangling), ...
			return ErrDestNotEmpty
		}
		entries, rerr := os.ReadDir(dest)
		if rerr != nil {
			return rerr
		}
		if len(entries) > 0 {
			return ErrDestNotEmpty
		}
	}
	_, err := r.Run(ctx, gitexec.Cmd{Dir: parent, Args: []string{"clone", "--progress", "--", url, dest}, OnStderr: withProgress(p)})
	if err != nil {
		if created {
			_ = os.RemoveAll(dest)
		} else if fi, lerr := os.Lstat(dest); ctx.Err() != nil && lerr == nil && fi.IsDir() {
			// git removes its own partial clone on ordinary failures; only a
			// killed git leaves debris in the pre-existing (empty) directory.
			entries, _ := os.ReadDir(dest)
			for _, e := range entries {
				_ = os.RemoveAll(filepath.Join(dest, e.Name()))
			}
		}
	}
	return err
}

// Init creates dir if needed and initializes a repository in it.
func Init(ctx context.Context, r *gitexec.Runner, dir string) error {
	if dir == "" {
		return fmt.Errorf("invalid directory: %q", dir)
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("directory must be an absolute path: %q", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return run(ctx, r, dir, "init")
}
