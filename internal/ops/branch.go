package ops

import (
	"context"
	"fmt"
	"strings"

	"gitrais/internal/gitexec"
)

// run executes a mutating git command and discards its stdout.
func run(ctx context.Context, r *gitexec.Runner, dir string, args ...string) error {
	_, err := git(ctx, r, dir, nil, args...)
	return err
}

// ValidateBranchName checks name with `git check-ref-format --branch`. Names
// that start with "-" (would be parsed as options) or that use the "@{" syntax
// (which --branch would expand, e.g. "@{-1}") are rejected up front.
func ValidateBranchName(ctx context.Context, r *gitexec.Runner, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.Contains(name, "@{") {
		return fmt.Errorf("invalid branch name: %q", name)
	}
	if _, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"check-ref-format", "--branch", name}, Env: gitexec.ReadOnlyEnv}); err != nil {
		return fmt.Errorf("invalid branch name: %q", name)
	}
	return nil
}

// validateTagName checks that name is a valid tag name and not option-like.
func validateTagName(ctx context.Context, r *gitexec.Runner, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid tag name: %q", name)
	}
	if _, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"check-ref-format", "refs/tags/" + name}, Env: gitexec.ReadOnlyEnv}); err != nil {
		return fmt.Errorf("invalid tag name: %q", name)
	}
	return nil
}

// validateRemoteName rejects empty or option-like remote names.
func validateRemoteName(remote string) error {
	if remote == "" || strings.HasPrefix(remote, "-") {
		return fmt.Errorf("invalid remote name: %q", remote)
	}
	return nil
}

// CreateBranch creates branch name at start, optionally switching to it.
func CreateBranch(ctx context.Context, r *gitexec.Runner, dir, name, start string, checkout bool) error {
	if err := ValidateBranchName(ctx, r, dir, name); err != nil {
		return err
	}
	if checkout {
		return run(ctx, r, dir, "switch", "-c", name, "--end-of-options", start)
	}
	return run(ctx, r, dir, "branch", "--end-of-options", name, start)
}

// RenameBranch renames a local branch.
func RenameBranch(ctx context.Context, r *gitexec.Runner, dir, oldName, newName string) error {
	if err := ValidateBranchName(ctx, r, dir, oldName); err != nil {
		return err
	}
	if err := ValidateBranchName(ctx, r, dir, newName); err != nil {
		return err
	}
	return run(ctx, r, dir, "branch", "-m", "--end-of-options", oldName, newName)
}

// DeleteBranch deletes a local branch (-D when force, otherwise -d).
func DeleteBranch(ctx context.Context, r *gitexec.Runner, dir, name string, force bool) error {
	if err := ValidateBranchName(ctx, r, dir, name); err != nil {
		return err
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	return run(ctx, r, dir, "branch", flag, "--end-of-options", name)
}

// DeleteRemoteBranch deletes a branch on a remote.
func DeleteRemoteBranch(ctx context.Context, r *gitexec.Runner, dir, remote, branch string) error {
	if err := validateRemoteName(remote); err != nil {
		return err
	}
	if err := ValidateBranchName(ctx, r, dir, branch); err != nil {
		return err
	}
	return run(ctx, r, dir, "push", "--delete", "--end-of-options", remote, branch)
}

// SetUpstream sets the upstream of a local branch (e.g. "origin/main").
func SetUpstream(ctx context.Context, r *gitexec.Runner, dir, branch, upstream string) error {
	if err := ValidateBranchName(ctx, r, dir, branch); err != nil {
		return err
	}
	if upstream == "" || strings.HasPrefix(upstream, "-") {
		return fmt.Errorf("invalid upstream: %q", upstream)
	}
	return run(ctx, r, dir, "branch", "--set-upstream-to="+upstream, "--end-of-options", branch)
}

// CheckoutTarget identifies what to check out.
type CheckoutTarget struct {
	Kind string `json:"kind"` // "local" | "remote" | "commit"
	Name string `json:"name"` // main | origin/feature | hash
}

// Checkout switches to a local branch, a remote branch (creating a tracking
// branch unless a local one with the same short name exists) or a commit
// (detached HEAD).
func Checkout(ctx context.Context, r *gitexec.Runner, dir string, t CheckoutTarget) error {
	switch t.Kind {
	case "local":
		if err := ValidateBranchName(ctx, r, dir, t.Name); err != nil {
			return err
		}
		return run(ctx, r, dir, "switch", "--end-of-options", t.Name)
	case "commit":
		if t.Name == "" || strings.HasPrefix(t.Name, "-") {
			return fmt.Errorf("invalid commit: %q", t.Name)
		}
		return run(ctx, r, dir, "switch", "--detach", "--end-of-options", t.Name)
	case "remote":
		local, err := remoteShortName(ctx, r, dir, t.Name)
		if err != nil {
			return err
		}
		if err := ValidateBranchName(ctx, r, dir, local); err != nil {
			return err
		}
		_, err = r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"show-ref", "--verify", "-q", "refs/heads/" + local}, Env: gitexec.ReadOnlyEnv})
		if err == nil {
			return run(ctx, r, dir, "switch", "--end-of-options", local)
		}
		if gitexec.ExitCode(err) != 1 {
			return err
		}
		return run(ctx, r, dir, "switch", "--track", "-c", local, "--end-of-options", "refs/remotes/"+t.Name)
	}
	return fmt.Errorf("unknown checkout kind: %q", t.Kind)
}

// remoteShortName strips the remote prefix from "remote/branch". Remote names
// may contain "/", so the longest configured remote that prefixes name wins.
func remoteShortName(ctx context.Context, r *gitexec.Runner, dir, name string) (string, error) {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"remote"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		return "", err
	}
	best := ""
	for _, rem := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if rem != "" && len(rem) > len(best) && strings.HasPrefix(name, rem+"/") && len(name) > len(rem)+1 {
			best = rem
		}
	}
	if best == "" {
		return "", fmt.Errorf("invalid remote ref: %q", name)
	}
	return name[len(best)+1:], nil
}

// CreateTag creates a lightweight tag (empty message) or an annotated one. The
// message is passed on stdin so no editor is ever opened.
func CreateTag(ctx context.Context, r *gitexec.Runner, dir, name, target, message string) error {
	if err := validateTagName(ctx, r, dir, name); err != nil {
		return err
	}
	if target == "" || strings.HasPrefix(target, "-") {
		return fmt.Errorf("invalid tag target: %q", target)
	}
	if message != "" {
		_, err := r.Run(ctx, gitexec.Cmd{Dir: dir,
			Args:  []string{"tag", "-a", "-F", "-", "--cleanup=whitespace", "--end-of-options", name, target},
			Stdin: strings.NewReader(message)})
		return err
	}
	return run(ctx, r, dir, "tag", "--end-of-options", name, target)
}

// DeleteTag deletes a local tag.
func DeleteTag(ctx context.Context, r *gitexec.Runner, dir, name string) error {
	if err := validateTagName(ctx, r, dir, name); err != nil {
		return err
	}
	return run(ctx, r, dir, "tag", "-d", "--end-of-options", name)
}

// PushTag pushes a single tag to a remote.
func PushTag(ctx context.Context, r *gitexec.Runner, dir, remote, name string) error {
	if err := validateRemoteName(remote); err != nil {
		return err
	}
	if err := validateTagName(ctx, r, dir, name); err != nil {
		return err
	}
	return run(ctx, r, dir, "push", "--end-of-options", remote, "refs/tags/"+name)
}
