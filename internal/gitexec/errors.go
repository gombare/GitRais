package gitexec

import (
	"errors"
	"fmt"
	"strings"
)

type Kind string

const (
	KindUnknown        Kind = "Unknown"
	KindAuthFailed     Kind = "AuthFailed"
	KindNonFastForward Kind = "NonFastForward"
	KindConflict       Kind = "Conflict"
	KindDirtyWorktree  Kind = "DirtyWorktree"
	KindLocked         Kind = "Locked"
	KindNotARepo       Kind = "NotARepo"
	KindNetwork        Kind = "Network"
	KindHookRejected   Kind = "HookRejected"
)

type GitError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Kind     Kind
	err      error
}

func newGitError(args []string, exit int, stderr string, err error) *GitError {
	red := make([]string, len(args))
	for i, a := range args {
		red[i] = RedactURL(a)
	}
	return &GitError{Args: red, ExitCode: exit, Stderr: RedactURL(stderr), Kind: Classify(stderr), err: err}
}

func (e *GitError) Error() string {
	sub := ""
	if len(e.Args) > 0 {
		sub = e.Args[0]
	}
	line := strings.TrimSpace(e.Stderr)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return fmt.Sprintf("git %s: exit %d: %s", sub, e.ExitCode, line)
}

func (e *GitError) Unwrap() error { return e.err }

// Le regole sono valutate in ordine: le più specifiche prima.
var rules = []struct {
	kind     Kind
	patterns []string
}{
	{KindNotARepo, []string{"not a git repository"}},
	{KindHookRejected, []string{"hook declined"}},
	{KindAuthFailed, []string{"Authentication failed", "could not read Username", "could not read Password",
		"Permission denied (publickey", "Invalid username or password", "terminal prompts disabled",
		"The requested URL returned error: 401", "The requested URL returned error: 403"}},
	{KindNonFastForward, []string{"(fetch first)", "(non-fast-forward)", "Updates were rejected"}},
	{KindConflict, []string{"CONFLICT (", "Automatic merge failed", "could not apply"}},
	{KindDirtyWorktree, []string{"would be overwritten by", "Please commit your changes or stash them"}},
	{KindLocked, []string{".lock': File exists", "Unable to create '"}},
	{KindNetwork, []string{"Could not resolve host", "Connection refused", "Connection timed out",
		"Failed to connect", "Network is unreachable", "Could not read from remote repository"}},
}

func Classify(stderr string) Kind {
	for _, r := range rules {
		for _, p := range r.patterns {
			if strings.Contains(stderr, p) {
				return r.kind
			}
		}
	}
	return KindUnknown
}

func KindOf(err error) Kind {
	var ge *GitError
	if errors.As(err, &ge) {
		return ge.Kind
	}
	return KindUnknown
}

func ExitCode(err error) int {
	var ge *GitError
	if errors.As(err, &ge) {
		return ge.ExitCode
	}
	return -1
}
