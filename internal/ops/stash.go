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
	// git exits 0 and reports "No local changes to save" on stdout.
	out, err := git(ctx, r, dir, nil, args...)
	if err != nil {
		return err
	}
	if strings.Contains(string(out), "No local changes to save") {
		return ErrNothingToStash
	}
	return nil
}

// StashApply applies a stash and keeps it. Conflicts are returned as the
// original GitError.
func StashApply(ctx context.Context, r *gitexec.Runner, dir, ref string) error {
	return stashRefCmd(ctx, r, dir, "apply", ref)
}

// StashPop applies a stash and drops it on success; on conflict git keeps the
// stash and the error is returned.
func StashPop(ctx context.Context, r *gitexec.Runner, dir, ref string) error {
	return stashRefCmd(ctx, r, dir, "pop", ref)
}

// StashDrop removes a stash.
func StashDrop(ctx context.Context, r *gitexec.Runner, dir, ref string) error {
	return stashRefCmd(ctx, r, dir, "drop", ref)
}

func stashRefCmd(ctx context.Context, r *gitexec.Runner, dir, sub, ref string) error {
	if err := validateStashRef(ref); err != nil {
		return err
	}
	out, err := git(ctx, r, dir, nil, "stash", sub, "--end-of-options", ref)
	// git reports merge conflicts on stdout only, so the stderr-based
	// classification yields KindUnknown; reclassify them here.
	var ge *gitexec.GitError
	if err != nil && errors.As(err, &ge) && ge.Kind == gitexec.KindUnknown && strings.Contains(string(out), "CONFLICT (") {
		ge.Kind = gitexec.KindConflict
	}
	return err
}
