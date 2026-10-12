package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gitrais/internal/gitexec"
)

// CommitOptions describes a commit request.
type CommitOptions struct {
	Message string `json:"message"`
	Amend   bool   `json:"amend"`
}

var (
	// ErrEmptyMessage is returned when the commit message is blank.
	ErrEmptyMessage = errors.New("commit message is empty")
	// ErrNothingToAmend is returned when amending on an unborn branch.
	ErrNothingToAmend = errors.New("cannot amend: the branch has no commits yet")
)

// hookNames are the hooks that can reject a commit.
var hookNames = []string{"pre-commit", "prepare-commit-msg", "commit-msg"}

// Commit commits the index and returns the new HEAD hash. The message is
// passed on stdin (never as argv) and git's default cleanup for -F is kept
// (whitespace only: "#" lines are preserved). A failure classified as
// KindUnknown is reclassified as KindHookRejected when an executable commit
// hook exists; the original (redacted) stderr is kept for display.
func Commit(ctx context.Context, r *gitexec.Runner, dir string, o CommitOptions) (string, error) {
	if strings.TrimSpace(o.Message) == "" {
		return "", ErrEmptyMessage
	}
	args := []string{"commit", "-F", "-"}
	if o.Amend {
		ok, err := hasHead(ctx, r, dir)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", ErrNothingToAmend
		}
		args = append(args, "--amend")
	}
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: args, Stdin: strings.NewReader(o.Message)})
	if err != nil {
		var ge *gitexec.GitError
		if errors.As(err, &ge) && ge.Kind == gitexec.KindUnknown && hooksPresent(ctx, r, dir) {
			ge.Kind = gitexec.KindHookRejected
		}
		return "", err
	}
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "HEAD"}, Env: gitexec.ReadOnlyEnv})
	return strings.TrimSpace(string(out)), err
}

// LastCommitMessage returns the HEAD commit message (to prefill an amend), or
// "" on an unborn branch.
func LastCommitMessage(ctx context.Context, r *gitexec.Runner, dir string) (string, error) {
	ok, err := hasHead(ctx, r, dir)
	if err != nil || !ok {
		return "", err
	}
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"log", "-1", "--format=%B"}, Env: gitexec.ReadOnlyEnv})
	return strings.TrimRight(string(out), "\n"), err
}

// hooksPresent reports whether an executable commit hook exists in the hooks
// directory resolved by git (which honours core.hooksPath).
func hooksPresent(ctx context.Context, r *gitexec.Runner, dir string) bool {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "--git-path", "hooks"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		return false
	}
	hooks := strings.TrimSpace(string(out))
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(dir, hooks)
	}
	for _, h := range hookNames {
		if st, err := os.Stat(filepath.Join(hooks, h)); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}
