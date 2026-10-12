package ops

import (
	"errors"
	"os"
	"strings"
	"testing"

	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

func TestStashCycle(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	repo.Write("a.txt", "2\n")
	repo.Write("u.txt", "u\n")
	if err := StashPush(bg, repo.R, repo.Dir, "wip", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(repo.Path("u.txt")); !os.IsNotExist(err) {
		t.Fatal("untracked file not included in the stash")
	}
	if repo.Git("stash", "list", "--format=%gs") != "On main: wip" {
		t.Fatal("wrong stash message")
	}
	if err := StashApply(bg, repo.R, repo.Dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	repo.Git("checkout", "--", "a.txt")
	os.Remove(repo.Path("u.txt"))
	if err := StashPop(bg, repo.R, repo.Dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if repo.Git("stash", "list") != "" {
		t.Fatal("pop did not remove the stash")
	}
	repo.Git("stash", "push", "-q")
	if err := StashDrop(bg, repo.R, repo.Dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if repo.Git("stash", "list") != "" {
		t.Fatal("drop failed")
	}
}

func TestStashMessageStartingWithDash(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	repo.Write("a.txt", "2\n")
	if err := StashPush(bg, repo.R, repo.Dir, "--include-untracked", false); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("stash", "list", "--format=%gs"); got != "On main: --include-untracked" {
		t.Fatalf("message = %q", got)
	}
}

func TestStashNothingToStash(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	if err := StashPush(bg, repo.R, repo.Dir, "", false); !errors.Is(err, ErrNothingToStash) {
		t.Fatalf("err = %v, want ErrNothingToStash", err)
	}
}

func TestStashUnbornBranch(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	err := StashPush(bg, repo.R, repo.Dir, "x", true)
	if !errors.Is(err, ErrStashUnborn) {
		t.Fatalf("err = %v, want ErrStashUnborn", err)
	}
}

func TestStashInvalidRef(t *testing.T) {
	repo := testutil.NewRepo(t)
	for _, ref := range []string{"", "--all", "-q", "stash@{x}", "stash@{-1}", "HEAD", "stash@{0} --foo", "abc123", "stash@{0}\n"} {
		for name, f := range map[string]func() error{
			"apply": func() error { return StashApply(bg, repo.R, repo.Dir, ref) },
			"pop":   func() error { return StashPop(bg, repo.R, repo.Dir, ref) },
			"drop":  func() error { return StashDrop(bg, repo.R, repo.Dir, ref) },
		} {
			if err := f(); err == nil || !strings.Contains(err.Error(), "invalid stash ref") {
				t.Errorf("%s(%q): err = %v, want invalid stash ref", name, ref, err)
			}
		}
	}
}

func TestStashPopConflictKeepsStash(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	repo.Write("a.txt", "stashed\n")
	if err := StashPush(bg, repo.R, repo.Dir, "c", false); err != nil {
		t.Fatal(err)
	}
	repo.Write("a.txt", "other\n")
	repo.CommitAll("diverge")
	err := StashPop(bg, repo.R, repo.Dir, "stash@{0}")
	if err == nil {
		t.Fatal("expected conflict")
	}
	var ge *gitexec.GitError
	if !errors.As(err, &ge) {
		t.Fatalf("want GitError, got %T", err)
	}
	if ge.Kind != gitexec.KindConflict {
		t.Fatalf("kind = %v, want Conflict", ge.Kind)
	}
	if repo.Git("stash", "list") == "" {
		t.Fatal("stash was dropped despite the conflict")
	}
}
