package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gitrais/internal/gitexec"
)

var (
	// ErrNothingToStash is returned when the working tree has no changes to save.
	ErrNothingToStash = errors.New("no local changes to save")
	// ErrStashUnborn is returned when stashing on a branch with no commits yet.
	ErrStashUnborn = errors.New("cannot stash: the branch has no commits yet")
	// ErrStashNotFound is returned when a stash given by hash no longer exists.
	ErrStashNotFound = errors.New("stash no longer exists")
)

// stashRefRe accepts only "stash@{N}" or a full 40/64-hex commit hash.
var stashRefRe = regexp.MustCompile(`^(stash@\{[0-9]+\}|[0-9a-f]{40}|[0-9a-f]{64})$`)

func validateStashRef(ref string) error {
	if !stashRefRe.MatchString(ref) {
		return fmt.Errorf("invalid stash ref: %q", ref)
	}
	return nil
}

// StashPush saves local changes (optionally untracked files too). The message
// is the value of -m, so it can never be parsed as an option.
func StashPush(ctx context.Context, r *gitexec.Runner, dir, message string, includeUntracked bool) error {
	ok, err := hasHead(ctx, r, dir)
	if err != nil {
		return err
	}
	if !ok {
		return ErrStashUnborn
	}
	args := []string{"stash", "push"}
	if includeUntracked {
		args = append(args, "--include-untracked")
	}
	if message != "" {
		args = append(args, "-m", message)
	}
	// git exits 0 even with nothing to save, and its stdout echoes the user
	// message, so detect a no-op by comparing refs/stash before and after.
	before, err := stashTip(ctx, r, dir)
	if err != nil {
		return err
	}
	if _, err := git(ctx, r, dir, nil, args...); err != nil {
		return err
	}
	after, err := stashTip(ctx, r, dir)
	if err != nil {
		return err
	}
	if after == before {
		return ErrNothingToStash
	}
	return nil
}

// stashTip returns the hash refs/stash points to, or "" when there is no stash.
func stashTip(ctx context.Context, r *gitexec.Runner, dir string) (string, error) {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "-q", "--verify", "refs/stash"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		if gitexec.ExitCode(err) == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// StashApply applies a stash and keeps it. Conflicts are returned as the
// original GitError. Callers should pass the stash commit hash (stable), not
// stash@{N}, which shifts when other stashes are added or dropped.
func StashApply(ctx context.Context, r *gitexec.Runner, dir, ref string) error {
	return stashRefCmd(ctx, r, dir, "apply", ref)
}

// StashPop applies a stash and drops it on success; on conflict git keeps the
// stash and the error is returned. Callers should pass the stash hash; if it
// no longer exists ErrStashNotFound is returned.
func StashPop(ctx context.Context, r *gitexec.Runner, dir, ref string) error {
	return stashRefCmd(ctx, r, dir, "pop", ref)
}

// StashDrop removes a stash. Callers should pass the stash hash; if it no
// longer exists ErrStashNotFound is returned, so a stale list can never drop
// the wrong stash.
func StashDrop(ctx context.Context, r *gitexec.Runner, dir, ref string) error {
	return stashRefCmd(ctx, r, dir, "drop", ref)
}

// resolveStashRef maps a commit hash to the current "stash@{N}"; a
// "stash@{N}" ref is returned unchanged.
func resolveStashRef(ctx context.Context, r *gitexec.Runner, dir, ref string) (string, error) {
	if strings.HasPrefix(ref, "stash@{") {
		return ref, nil
	}
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"stash", "list", "--format=%H%x00%gd"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		hash, sel, ok := strings.Cut(strings.TrimSpace(line), "\x00")
		if ok && hash == ref {
			return sel, nil
		}
	}
	return "", ErrStashNotFound
}

func stashRefCmd(ctx context.Context, r *gitexec.Runner, dir, sub, ref string) error {
	if err := validateStashRef(ref); err != nil {
		return err
	}
	sel, err := resolveStashRef(ctx, r, dir, ref)
	if err != nil {
		return err
	}
	_, err = git(ctx, r, dir, nil, "stash", sub, "--end-of-options", sel)
	// git reports merge conflicts on stdout (depending on merge.verbosity), so
	// the stderr-based classification yields KindUnknown; reclassify whenever
	// the index holds unmerged entries.
	var ge *gitexec.GitError
	if err != nil && errors.As(err, &ge) && ge.Kind == gitexec.KindUnknown {
		u, uerr := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"ls-files", "-u", "-z"}, Env: gitexec.ReadOnlyEnv})
		if uerr == nil && len(u) > 0 {
			ge.Kind = gitexec.KindConflict
		}
	}
	return err
}
