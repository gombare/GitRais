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
	mustStageAll(t, repo)
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
	writeHook(t, hook, "#!/bin/sh\necho 'lint failed' >&2\nexit 1\n", 0o755)
	repo.Write("a.txt", "1\n")
	mustStageAll(t, repo)
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
	writeHook(t, hook, "#!/bin/sh\nexit 1\n", 0o644)
	// Nothing staged: the commit fails for a reason unrelated to the hook.
	_, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "x"})
	if err == nil || gitexec.KindOf(err) == gitexec.KindHookRejected {
		t.Fatalf("want non-hook error, got %v", err)
	}
}

func TestCommitUnbornAndAmendUnborn(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	mustStageAll(t, repo)
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
	mustStageAll(t, repo)
	m := "--amend -m\n\n# not a comment\nline"
	if _, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: m}); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("log", "-1", "--format=%B"); got != m {
		t.Fatalf("message altered: %q", got)
	}
}

func mustStageAll(t *testing.T, repo *testutil.Repo) {
	t.Helper()
	if err := StageAll(bg, repo.R, repo.Dir); err != nil {
		t.Fatal(err)
	}
}

func writeHook(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestCommitNothingToCommitWithHook(t *testing.T) {
	repo := testutil.NewRepo(t)
	writeHook(t, filepath.Join(repo.Dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\nexit 0\n", 0o755)
	_, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "x"})
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("want ErrNothingToCommit, got %v", err)
	}
}

func TestCommitIdentityFailureNotHookRejected(t *testing.T) {
	repo := testutil.NewRepo(t)
	writeHook(t, filepath.Join(repo.Dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\nexit 0\n", 0o755)
	repo.Git("config", "user.useConfigOnly", "true")
	repo.Write("a.txt", "1\n")
	mustStageAll(t, repo)
	// Drop the identity the isolated environment provides via env vars.
	r := *repo.R
	r.Env = nil
	for _, e := range repo.R.Env {
		if !strings.HasPrefix(e, "GIT_AUTHOR_") && !strings.HasPrefix(e, "GIT_COMMITTER_") {
			r.Env = append(r.Env, e)
		}
	}
	_, err := Commit(bg, &r, repo.Dir, CommitOptions{Message: "x"})
	if err == nil || gitexec.KindOf(err) == gitexec.KindHookRejected {
		t.Fatalf("want non-hook error, got %v", err)
	}
}

func TestLastCommitMessageIgnoresShowSignature(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	mustStageAll(t, repo)
	if _, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "subject\n\nbody"}); err != nil {
		t.Fatal(err)
	}
	repo.Git("config", "log.showSignature", "true")
	msg, err := LastCommitMessage(bg, repo.R, repo.Dir)
	if err != nil || msg != "subject\n\nbody" {
		t.Fatalf("got %q %v", msg, err)
	}
}

func TestCommitOnlyUntrackedWithHook(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "a\n")
	repo.CommitAll("base")
	repo.Write("new.txt", "n\n")
	writeHook(t, filepath.Join(repo.Dir, ".git", "hooks", "pre-commit"), "#!/bin/sh\nexit 0\n", 0o755)
	_, err := Commit(bg, repo.R, repo.Dir, CommitOptions{Message: "x"})
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("want ErrNothingToCommit, got %v", err)
	}
}
