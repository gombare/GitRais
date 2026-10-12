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

// pushStashes creates n stashes (oldest first) and returns their hashes,
// newest first (index i is stash@{i}).
func pushStashes(t *testing.T, repo *testutil.Repo, n int) []string {
	t.Helper()
	repo.Write("a.txt", "base\n")
	repo.CommitAll("base")
	for i := 0; i < n; i++ {
		repo.Write("a.txt", "change"+string(rune('0'+i))+"\n")
		if err := StashPush(bg, repo.R, repo.Dir, "s"+string(rune('0'+i)), false); err != nil {
			t.Fatal(err)
		}
	}
	return strings.Fields(repo.Git("stash", "list", "--format=%H"))
}

func TestStashDropByHashSurvivesShift(t *testing.T) {
	repo := testutil.NewRepo(t)
	hashes := pushStashes(t, repo, 3)
	middle := hashes[1]
	if err := StashDrop(bg, repo.R, repo.Dir, "stash@{0}"); err != nil {
		t.Fatal(err)
	}
	if err := StashDrop(bg, repo.R, repo.Dir, middle); err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(repo.Git("stash", "list", "--format=%H"))
	if len(got) != 1 || got[0] != hashes[2] {
		t.Fatalf("remaining = %v, want only %s", got, hashes[2])
	}
}

func TestStashPopByHash(t *testing.T) {
	repo := testutil.NewRepo(t)
	hashes := pushStashes(t, repo, 2)
	if err := StashPop(bg, repo.R, repo.Dir, hashes[1]); err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(repo.Git("stash", "list", "--format=%H"))
	if len(got) != 1 || got[0] != hashes[0] {
		t.Fatalf("remaining = %v, want only %s", got, hashes[0])
	}
	if b, _ := os.ReadFile(repo.Path("a.txt")); string(b) != "change0\n" {
		t.Fatalf("a.txt = %q", b)
	}
}

func TestStashUnknownHash(t *testing.T) {
	repo := testutil.NewRepo(t)
	pushStashes(t, repo, 1)
	h := strings.Repeat("a", 40)
	for name, err := range map[string]error{
		"apply": StashApply(bg, repo.R, repo.Dir, h),
		"pop":   StashPop(bg, repo.R, repo.Dir, h),
		"drop":  StashDrop(bg, repo.R, repo.Dir, h),
	} {
		if !errors.Is(err, ErrStashNotFound) {
			t.Errorf("%s: err = %v, want ErrStashNotFound", name, err)
		}
	}
}

func TestStashMessageEchoingNothingToSave(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	repo.Write("a.txt", "2\n")
	if err := StashPush(bg, repo.R, repo.Dir, "No local changes to save", false); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if repo.Git("stash", "list") == "" {
		t.Fatal("stash not created")
	}
}

func TestStashConflictQuietMerge(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Git("config", "merge.verbosity", "0")
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	repo.Write("a.txt", "stashed\n")
	if err := StashPush(bg, repo.R, repo.Dir, "c", false); err != nil {
		t.Fatal(err)
	}
	repo.Write("a.txt", "other\n")
	repo.CommitAll("diverge")
	err := StashPop(bg, repo.R, repo.Dir, "stash@{0}")
	var ge *gitexec.GitError
	if !errors.As(err, &ge) || ge.Kind != gitexec.KindConflict {
		t.Fatalf("err = %v, want KindConflict", err)
	}
	if repo.Git("stash", "list") == "" {
		t.Fatal("stash was dropped despite the conflict")
	}
}
