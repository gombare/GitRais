package gitexec

import (
	"errors"
	"fmt"
	"regexp"
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
	line := summarize(e.Stderr)
	return fmt.Sprintf("git %s: exit %d: %s", sub, e.ExitCode, line)
}

var noiseRE = regexp.MustCompile(`^(?:remote: )?(?:Enumerating|Counting|Compressing|Writing|Receiving|Resolving|Total|Delta compression|Cloning into)\b`)

// summarize picks the most meaningful stderr line for Error(): git run with
// --progress prepends progress noise, so prefer fatal:/error:/rejection lines,
// skipping progress output, and fall back to the first non-noise line.
func summarize(stderr string) string {
	var fallback, first string
	// Progress updates end in \r, so an error printed mid-progress shares a
	// \n-line with them: judge every \r/\n segment on its own.
	segments := strings.FieldsFunc(stderr, func(r rune) bool { return r == '\n' || r == '\r' })
	for _, raw := range segments {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if first == "" {
			first = line
		}
		if noiseRE.MatchString(line) {
			continue
		}
		if _, ok := ParseProgressLine(line); ok {
			continue
		}
		msg := strings.TrimPrefix(line, "remote: ")
		if strings.HasPrefix(msg, "fatal:") || strings.HasPrefix(msg, "error:") || strings.HasPrefix(line, "! [") {
			return line
		}
		if fallback == "" {
			fallback = line
		}
	}
	if fallback != "" {
		return fallback
	}
	return first
}

func (e *GitError) Unwrap() error { return e.err }

// Rules are evaluated in order: the most specific first.
var rules = []struct {
	kind     Kind
	patterns []string
}{
	{KindNotARepo, []string{"not a git repository"}},
	{KindHookRejected, []string{"hook declined"}},
	{KindAuthFailed, []string{"Authentication failed", "could not read Username", "could not read Password",
		"Permission denied (publickey", "Invalid username or password", "terminal prompts disabled",
		"The requested URL returned error: 401", "The requested URL returned error: 403"}},
	{KindNonFastForward, []string{"(fetch first)", "(non-fast-forward)", "Updates were rejected", "(stale info)"}},
	{KindConflict, []string{"CONFLICT (", "Automatic merge failed", "could not apply"}},
	{KindDirtyWorktree, []string{"would be overwritten by", "Please commit your changes or stash them",
		"cannot pull with rebase: You have unstaged changes", "cannot pull with rebase: Your index contains uncommitted changes"}},
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
