package session

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"gitrais/internal/diff"
	"gitrais/internal/gitexec"
	"gitrais/internal/gitread"
	"gitrais/internal/ops"
)

var (
	// ErrPartialStageUnsupported is returned by StageLines for anything but a
	// modified text file (added, deleted, renamed, conflicted, binary).
	ErrPartialStageUnsupported = errors.New("partial staging is only available for modified text files")
	// ErrHunkOutOfRange is returned by StageLines when the diff changed since the
	// frontend read it.
	ErrHunkOutOfRange = errors.New("invalid hunk: the file changed, reload the diff")
	// ErrLineOutOfRange is returned by StageLines for a line index outside the
	// selected hunk.
	ErrLineOutOfRange = errors.New("invalid line index")
	// ErrNoLinesSelected is returned by StageLines when the selection holds no
	// added or removed line.
	ErrNoLinesSelected = errors.New("no changed lines selected")
	// ErrDiffChanged is returned by StageLines when the hunk no longer matches
	// the fingerprint the frontend sent: the file changed since it was shown.
	ErrDiffChanged = errors.New("the diff changed since it was shown, reload it")
	// ErrNoBranch is returned by PushCurrent with a detached or unborn HEAD.
	ErrNoBranch = errors.New("no current branch to push")
	// ErrNoRemote is returned by PushCurrent when no remote is configured.
	ErrNoRemote = errors.New("no remote configured")
	// ErrUpstreamNotRemote is returned by PushCurrent when the branch tracks
	// something that is not a branch of a configured remote.
	ErrUpstreamNotRemote = errors.New("the current branch's upstream is not a remote branch")
)

// StageLinesRequest stages (Staged=false) or unstages (Staged=true) a hunk or
// some of its lines. Hunk and Lines index the FileDiff returned by WipDiff with
// the same Path and Staged.
type StageLinesRequest struct {
	Path   string `json:"path"`
	Staged bool   `json:"staged"`
	Hunk   int    `json:"hunk"`
	Lines  []int  `json:"lines"` // empty = whole hunk
	// Expect, when set, must match the re-read hunk or nothing is staged
	// (ErrDiffChanged). The frontend always sends it.
	Expect *HunkFingerprint `json:"expect,omitempty"`
}

// HunkFingerprint is what the frontend saw of the hunk being staged: its
// header and, for each index in Lines (or each hunk line when Lines is
// empty), in the same order, the line's kind and text.
type HunkFingerprint struct {
	OldStart int               `json:"oldStart"`
	OldLines int               `json:"oldLines"`
	NewStart int               `json:"newStart"`
	NewLines int               `json:"newLines"`
	Lines    []FingerprintLine `json:"lines"`
}

// FingerprintLine is the kind ("+", "-", " " or "\") and text of a hunk line.
type FingerprintLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// matches reports whether h, with the selected line indexes (all lines when
// empty, already range checked), is the hunk the fingerprint describes.
func (fp *HunkFingerprint) matches(h diff.Hunk, lines []int) bool {
	if fp.OldStart != h.OldStart || fp.OldLines != h.OldLines || fp.NewStart != h.NewStart || fp.NewLines != h.NewLines {
		return false
	}
	idx := lines
	if len(idx) == 0 {
		idx = make([]int, len(h.Lines))
		for i := range idx {
			idx[i] = i
		}
	}
	if len(fp.Lines) != len(idx) {
		return false
	}
	for k, i := range idx {
		if fp.Lines[k].Kind != string(h.Lines[i].Kind) || jsonText(fp.Lines[k].Text) != jsonText(h.Lines[i].Text) {
			return false
		}
	}
	return true
}

// jsonText normalises text the way encoding/json does on the way to the
// frontend: each invalid UTF-8 byte becomes U+FFFD. The fingerprint comes back
// in that form, so non-UTF-8 files (e.g. Latin-1) must be compared the same way.
func jsonText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// RemoveIndexLock removes a stale <gitdir>/index.lock (see ops.RemoveIndexLock).
// The path comes from the session, never from the caller.
func (s *Session) RemoveIndexLock(ctx context.Context) error {
	return s.write(ctx, false, func(c context.Context) error { return ops.RemoveIndexLock(c, s.GitDir, s.Dir) })
}

func (s *Session) Stage(ctx context.Context, paths []string) error {
	return s.write(ctx, false, func(c context.Context) error { return ops.StageFiles(c, s.r, s.Dir, paths) })
}

func (s *Session) Unstage(ctx context.Context, paths []string) error {
	return s.write(ctx, false, func(c context.Context) error { return ops.UnstageFiles(c, s.r, s.Dir, paths) })
}

func (s *Session) StageAll(ctx context.Context) error {
	return s.write(ctx, false, func(c context.Context) error { return ops.StageAll(c, s.r, s.Dir) })
}

func (s *Session) UnstageAll(ctx context.Context) error {
	return s.write(ctx, false, func(c context.Context) error { return ops.UnstageAll(c, s.r, s.Dir) })
}

func (s *Session) Discard(ctx context.Context, tracked, untracked []string) error {
	return s.write(ctx, false, func(c context.Context) error { return ops.DiscardFiles(c, s.r, s.Dir, tracked, untracked) })
}

// StageLines stages or unstages part of a modified file. The diff is re-read
// under the write lock, so the indices must match the current state, and the
// hunk is checked against req.Expect so a file changed since the frontend
// loaded its diff is never staged by stale indices.
func (s *Session) StageLines(ctx context.Context, req StageLinesRequest) error {
	return s.write(ctx, false, func(c context.Context) error {
		fd, err := ops.WipDiff(c, s.r, s.Dir, ops.WipDiffRequest{Path: req.Path, Staged: req.Staged})
		if err != nil {
			return err
		}
		// "U" (conflicted), "A", "D", "R", ... have no partial staging.
		if fd.Binary || fd.Status != "M" {
			return ErrPartialStageUnsupported
		}
		if req.Hunk < 0 || req.Hunk >= len(fd.Hunks) {
			return ErrHunkOutOfRange
		}
		sel := diff.SelectHunk(fd, req.Hunk)
		if len(req.Lines) > 0 {
			sel = diff.Selection{req.Hunk: {}}
			for _, l := range req.Lines {
				if l < 0 || l >= len(fd.Hunks[req.Hunk].Lines) {
					return ErrLineOutOfRange
				}
				sel[req.Hunk][l] = true
			}
		}
		if req.Expect != nil && !req.Expect.matches(fd.Hunks[req.Hunk], req.Lines) {
			return ErrDiffChanged
		}
		mode, reverse := diff.ForStage, false
		if req.Staged {
			mode, reverse = diff.ForUnstage, true
		}
		patch, ok := diff.BuildPatch(fd, sel, mode)
		if !ok {
			return ErrNoLinesSelected
		}
		return ops.ApplyPatch(c, s.r, s.Dir, patch, reverse)
	})
}

// CommitChanges commits the index and returns the new commit hash.
func (s *Session) CommitChanges(ctx context.Context, o ops.CommitOptions) (string, error) {
	var hash string
	err := s.write(ctx, true, func(c context.Context) error {
		var err error
		hash, err = ops.Commit(c, s.r, s.Dir, o)
		return err
	})
	return hash, err
}

func (s *Session) CreateBranch(ctx context.Context, name, start string, checkout bool) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.CreateBranch(c, s.r, s.Dir, name, start, checkout) })
}

func (s *Session) RenameBranch(ctx context.Context, oldName, newName string) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.RenameBranch(c, s.r, s.Dir, oldName, newName) })
}

func (s *Session) DeleteBranch(ctx context.Context, name string, force bool) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.DeleteBranch(c, s.r, s.Dir, name, force) })
}

// DeleteRemoteBranch is a remote operation (exclusive, cancellable).
func (s *Session) DeleteRemoteBranch(ctx context.Context, remote, branch string) error {
	return s.remote(ctx, "delete-remote-branch", func(c context.Context, _ ops.OnProgress) error {
		return ops.DeleteRemoteBranch(c, s.r, s.Dir, remote, branch)
	})
}

func (s *Session) SetUpstream(ctx context.Context, branch, upstream string) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.SetUpstream(c, s.r, s.Dir, branch, upstream) })
}

func (s *Session) Checkout(ctx context.Context, t ops.CheckoutTarget) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.Checkout(c, s.r, s.Dir, t) })
}

func (s *Session) CreateTag(ctx context.Context, name, target, message string) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.CreateTag(c, s.r, s.Dir, name, target, message) })
}

func (s *Session) DeleteTag(ctx context.Context, name string) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.DeleteTag(c, s.r, s.Dir, name) })
}

// PushTag is a remote operation (exclusive, cancellable).
func (s *Session) PushTag(ctx context.Context, remote, name string) error {
	return s.remote(ctx, "push-tag", func(c context.Context, _ ops.OnProgress) error {
		return ops.PushTag(c, s.r, s.Dir, remote, name)
	})
}

func (s *Session) StashPush(ctx context.Context, message string, untracked bool) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.StashPush(c, s.r, s.Dir, message, untracked) })
}

// StashApply applies the stash with the given commit hash (graph.Stash.Hash),
// resolved to its current stash@{N}; ops.ErrStashNotFound if it is gone.
func (s *Session) StashApply(ctx context.Context, hash string) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.StashApply(c, s.r, s.Dir, hash) })
}

// StashPop is StashApply followed by a drop on success.
func (s *Session) StashPop(ctx context.Context, hash string) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.StashPop(c, s.r, s.Dir, hash) })
}

// StashDrop drops the stash with the given commit hash.
func (s *Session) StashDrop(ctx context.Context, hash string) error {
	return s.write(ctx, true, func(c context.Context) error { return ops.StashDrop(c, s.r, s.Dir, hash) })
}

// Fetch fetches all remotes. Remote operation: exclusive (ErrOpInProgress),
// cancellable with CancelOp, progress as op:progress. Success starts the
// auto-fetch (see manualRemoteDone) and resumes it if paused.
func (s *Session) Fetch() error {
	err := s.remote(context.Background(), "fetch", func(c context.Context, p ops.OnProgress) error { return ops.Fetch(c, s.r, s.Dir, p) })
	s.manualRemoteDone(err, true, true)
	return err
}

// Pull pulls the current branch; ops.ErrPullStrategyRequired asks the user to
// choose a mode when branches diverged.
func (s *Session) Pull(mode ops.PullMode) error {
	err := s.remote(context.Background(), "pull", func(c context.Context, p ops.OnProgress) error { return ops.Pull(c, s.r, s.Dir, mode, p) })
	s.manualRemoteDone(err, false, true)
	return err
}

// PullAutostash is Pull with git's --autostash: local changes are stashed and
// re-applied around the integration. ops.ErrAutostashConflict when the
// re-application conflicts (the stash entry is kept). Opt-in only: the UI
// offers it after Pull failed with KindDirtyWorktree.
func (s *Session) PullAutostash(mode ops.PullMode) error {
	err := s.remote(context.Background(), "pull", func(c context.Context, p ops.OnProgress) error {
		return ops.PullAutostash(c, s.r, s.Dir, mode, p)
	})
	s.manualRemoteDone(err, false, true)
	return err
}

// PushCurrent pushes the current branch to its upstream or, when it has none,
// to origin (or the first remote), setting the upstream.
func (s *Session) PushCurrent(force bool) error {
	if s.ctx.Err() != nil {
		return ErrClosed
	}
	o, err := s.pushTarget(s.ctx, force)
	if err != nil {
		return err
	}
	err = s.remote(context.Background(), "push", func(c context.Context, p ops.OnProgress) error { return ops.Push(c, s.r, s.Dir, o, p) })
	s.manualRemoteDone(err, false, false)
	return err
}

func (s *Session) pushTarget(ctx context.Context, force bool) (ops.PushOptions, error) {
	refs, err := gitread.ReadRefs(ctx, s.r, s.Dir)
	if err != nil {
		return ops.PushOptions{}, err
	}
	if refs.Head.Detached || refs.Head.Unborn || refs.Head.Branch == "" {
		return ops.PushOptions{}, ErrNoBranch
	}
	out, err := s.r.Run(ctx, gitexec.Cmd{Dir: s.Dir, Args: []string{"remote"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		return ops.PushOptions{}, err
	}
	remotes := strings.Fields(string(out))
	o := ops.PushOptions{Branch: refs.Head.Branch, ForceWithLease: force}
	upstream := ""
	for _, r := range refs.Refs {
		if r.IsHead && r.Kind == gitread.RefLocal {
			upstream = r.Upstream
		}
	}
	if upstream != "" {
		// The longest matching remote name wins, so names containing "/" work.
		for _, rm := range remotes {
			if b, ok := strings.CutPrefix(upstream, rm+"/"); ok && b != "" && len(rm) > len(o.Remote) {
				o.Remote, o.RemoteBranch = rm, b
			}
		}
		if o.Remote == "" {
			return ops.PushOptions{}, ErrUpstreamNotRemote
		}
		return o, nil
	}
	if len(remotes) == 0 {
		return ops.PushOptions{}, ErrNoRemote
	}
	o.Remote, o.SetUpstream = remotes[0], true
	if slices.Contains(remotes, "origin") {
		o.Remote = "origin"
	}
	return o, nil
}
