package app

import (
	"context"
	"encoding/json"
	"errors"

	"gitrais/internal/gitexec"
	"gitrais/internal/ops"
	"gitrais/internal/session"
	"gitrais/internal/store"
)

// UIError is the JSON carried by every error returned to the frontend (the
// rejected promise's message). Kind is a stable identifier the frontend can
// switch on; Message is human readable; Stderr is git's (redacted) stderr when
// the error comes from a git command.
type UIError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Stderr  string `json:"stderr"`
}

var (
	errNoTab       = errors.New("repository tab not found")
	errGitMissing  = errors.New("the bundled git was not found: repository operations are unavailable")
	errInvalidPath = errors.New("the path must be absolute and non-empty")
)

// sentinelKinds maps backend sentinel errors to UIError kinds. It is checked
// in order with errors.Is, before the git error classification.
var sentinelKinds = []struct {
	err  error
	kind string
}{
	{errNoTab, "NoTab"},
	{errGitMissing, "GitMissing"},
	{errInvalidPath, "InvalidPath"},
	{ops.ErrInvalidFilePath, "InvalidFilePath"},
	{ops.ErrEmptyMessage, "EmptyMessage"},
	{ops.ErrNothingToCommit, "NothingToCommit"},
	{ops.ErrNothingToAmend, "NothingToAmend"},
	{ops.ErrNothingToStash, "NothingToStash"},
	{ops.ErrStashUnborn, "StashUnborn"},
	{ops.ErrStashNotFound, "StashNotFound"},
	{ops.ErrPullStrategyRequired, "PullStrategyRequired"},
	{ops.ErrDestNotEmpty, "DestNotEmpty"},
	{ops.ErrLockInUse, "LockInUse"},
	{ops.ErrLockTooRecent, "LockTooRecent"},
	{ops.ErrLsofMissing, "LsofMissing"},
	{session.ErrClosed, "Cancelled"},
	{context.Canceled, "Cancelled"},
	{context.DeadlineExceeded, "Timeout"},
	{session.ErrOpInProgress, "OpInProgress"},
	{session.ErrInvalidHash, "InvalidHash"},
	{session.ErrPartialStageUnsupported, "PartialStageUnsupported"},
	{session.ErrHunkOutOfRange, "HunkOutOfRange"},
	{session.ErrDiffChanged, "DiffChanged"},
	{session.ErrLineOutOfRange, "LineOutOfRange"},
	{session.ErrNoLinesSelected, "NoLinesSelected"},
	{session.ErrNoBranch, "NoBranch"},
	{session.ErrNoRemote, "NoRemote"},
	{session.ErrUpstreamNotRemote, "UpstreamNotRemote"},
	{store.ErrCorrupt, "StateCorrupt"},
}

// jsonError is an error whose text is a UIError JSON document.
type jsonError struct{ text string }

func (e *jsonError) Error() string { return e.text }

// toUIError classifies err. Messages and stderr pass through RedactURL so no
// credential embedded in a URL reaches the UI (GitError is already redacted).
func toUIError(err error) UIError {
	ue := UIError{Kind: string(gitexec.KindUnknown), Message: gitexec.RedactURL(err.Error())}
	var ge *gitexec.GitError
	if errors.As(err, &ge) {
		ue.Kind = string(ge.Kind)
		ue.Stderr = gitexec.RedactURL(ge.Stderr)
	}
	for _, s := range sentinelKinds {
		if errors.Is(err, s.err) {
			ue.Kind = s.kind
			break
		}
	}
	return ue
}

// uiErr converts a backend error into an error whose text is UIError JSON. It
// is idempotent: an already converted error is returned unchanged.
func uiErr(err error) error {
	if err == nil {
		return nil
	}
	var je *jsonError
	if errors.As(err, &je) {
		return je
	}
	b, _ := json.Marshal(toUIError(err))
	return &jsonError{text: string(b)}
}
