package ops

import (
	"bytes"
	"context"
	"fmt"

	"gitrais/internal/diff"
	"gitrais/internal/gitexec"
)

// WipDiffRequest selects which side of the working-tree state to diff.
type WipDiffRequest struct {
	Path      string `json:"path"`
	Staged    bool   `json:"staged"`
	Untracked bool   `json:"untracked"`
}

// diffBase pins the path prefixes so user config (diff.noprefix,
// diff.mnemonicPrefix) can't change what diff.Parse and BuildPatch expect, and
// disables textconv so hunks carry the real bytes (needed to build patches).
var diffBase = []string{
	"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M",
	"--src-prefix=a/", "--dst-prefix=b/",
}

var diffEnv = append(append([]string{}, gitexec.ReadOnlyEnv...), literalPaths...)

// WipDiff returns the diff of one working-tree file: unstaged (index to
// worktree), staged (HEAD to index) or untracked (against /dev/null).
func WipDiff(ctx context.Context, r *gitexec.Runner, dir string, req WipDiffRequest) (diff.FileDiff, error) {
	args := append([]string{}, diffBase...)
	switch {
	case req.Untracked:
		args = append(args, "--no-index", "--", "/dev/null", req.Path)
	case req.Staged:
		args = append(args, "--cached", "--", req.Path)
	default:
		args = append(args, "--", req.Path)
	}
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: args, Env: diffEnv})
	// --no-index exits with 1 when the files differ.
	if err != nil && !(req.Untracked && gitexec.ExitCode(err) == 1) {
		return diff.FileDiff{}, err
	}
	return single(out, req.Path)
}

// CommitFileDiff returns the diff of one file between two revisions. oldPath
// is the pre-rename path, if any.
func CommitFileDiff(ctx context.Context, r *gitexec.Runner, dir, from, to, path, oldPath string) (diff.FileDiff, error) {
	args := append(append([]string{}, diffBase...), "--end-of-options", from, to, "--", path)
	if oldPath != "" && oldPath != path {
		args = append(args, oldPath)
	}
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: args, Env: diffEnv})
	if err != nil {
		return diff.FileDiff{}, err
	}
	return single(out, path)
}

func single(out []byte, path string) (diff.FileDiff, error) {
	files, err := diff.Parse(out)
	if err != nil {
		return diff.FileDiff{}, err
	}
	switch len(files) {
	case 0:
		// "git diff --cached" prints only "* Unmerged path <p>" for conflicts.
		status := "M"
		if bytes.Contains(out, []byte("* Unmerged path ")) {
			status = "U"
		}
		return diff.FileDiff{NewPath: path, OldPath: path, Status: status, Hunks: []diff.Hunk{}}, nil
	case 1:
		return files[0], nil
	}
	for _, f := range files {
		if f.NewPath == path {
			return f, nil
		}
	}
	return diff.FileDiff{}, fmt.Errorf("diff: %s not found in output", path)
}
