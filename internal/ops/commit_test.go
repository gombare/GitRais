package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

func TestCommitAndAmend(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	StageAll(bg, repo.R, repo.Dir)
	h, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "first\n\nbody"})
	if err != nil {
		t.Fatal(err)
	}
	if h != repo.Git("rev-parse", "HEAD") || repo.Git("log", "-1", "--format=%B") != "first\n\nbody" {
		t.Fatal("commit not created correctly")
	}
	msg, _ := LastCommitMessage(bg, repo.R, repo.Dir)
	if msg != "first\n\nbody" {
		t.Fatalf("last message %q", msg)
	}
	h2, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "first amended", Amend: true})
	if err != nil || h2 == h || repo.Git("rev-list", "--count", "HEAD") != "1" {
		t.Fatalf("amend: %v", err)
	}
	if _, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "  \n"}); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("want ErrEmptyMessage, got %v", err)
	}
}

func TestCommitHookRejected(t *testing.T) {
	repo := testutil.NewRepo(t)
	hook := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")
	os.MkdirAll(filepath.Dir(hook), 0o755)
	os.WriteFile(hook, []byte("#!/bin/sh\necho 'lint failed' >&2\nexit 1\n"), 0o755)
	repo.Write("a.txt", "1\n")
	StageAll(bg, repo.R, repo.Dir)
	_, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "x"})
	var ge *gitexec.GitError
	if !errors.As(err, &ge) || ge.Kind != gitexec.KindHookRejected || ge.Stderr == "" {
		t.Fatalf("want HookRejected with output, got %v", err)
	}
	if !strings.Contains(ge.Stderr, "lint failed") {
		t.Fatalf("hook output lost: %q", ge.Stderr)
	}
}

func TestCommitNonExecutableHookNotReclassified(t *testing.T) {
	repo := testutil.NewRepo(t)
	hook := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")
	os.MkdirAll(filepath.Dir(hook), 0o755)
	os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o644)
	// Nothing staged: the commit fails for a reason unrelated to the hook.
	_, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "x"})
	if err == nil || gitexec.KindOf(err) == gitexec.KindHookRejected {
		t.Fatalf("want non-hook error, got %v", err)
	}
}

func TestCommitUnbornAndAmendUnborn(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	StageAll(bg, repo.R, repo.Dir)
	if _, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "m", Amend: true}); !errors.Is(err, ErrNothingToAmend) {
		t.Fatalf("want ErrNothingToAmend, got %v", err)
	}
	if msg, err := LastCommitMessage(bg, repo.R, repo.Dir); err != nil || msg != "" {
		t.Fatalf("unborn last message: %q %v", msg, err)
	}
	h, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "root"})
	if err != nil || h != repo.Git("rev-parse", "HEAD") {
		t.Fatalf("unborn commit: %v", err)
	}
}

func TestCommitMessageVerbatim(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	StageAll(bg, repo.R, repo.Dir)
	m := "--amend -m\n\n# not a comment\nline"
	if _, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: m}); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("log", "-1", "--format=%B"); got != m {
		t.Fatalf("message altered: %q", got)
	}
}
