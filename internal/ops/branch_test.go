package ops

import (
	"strings"
	"testing"

	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

func TestBranchLifecycle(t *testing.T) {
	repo := testutil.NewRepo(t)
	base := repo.CommitAll("base")
	if err := ValidateBranchName(bg, repo.R, repo.Dir, "bad..name"); err == nil {
		t.Fatal("invalid name accepted")
	}
	if err := CreateBranch(bg, repo.R, repo.Dir, "feat", base, true); err != nil {
		t.Fatal(err)
	}
	if repo.Git("branch", "--show-current") != "feat" {
		t.Fatal("checkout after create did not happen")
	}
	if err := Checkout(bg, repo.R, repo.Dir, CheckoutTarget{Kind: "local", Name: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := RenameBranch(bg, repo.R, repo.Dir, "feat", "feature"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBranch(bg, repo.R, repo.Dir, "feature", false); err != nil {
		t.Fatal(err)
	}
	if repo.Git("branch", "--list", "feature") != "" {
		t.Fatal("branch not deleted")
	}
}

func TestCheckoutRemoteAndDetached(t *testing.T) {
	a := testutil.NewRepo(t)
	h := a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "origin", "main")
	a.Git("push", "-q", "origin", "main:topic")
	b := testutil.NewRepoWith(t, a.R)
	b.Git("remote", "add", "origin", bare)
	b.Git("fetch", "-q", "origin")
	if err := Checkout(bg, b.R, b.Dir, CheckoutTarget{Kind: "remote", Name: "origin/topic"}); err != nil {
		t.Fatal(err)
	}
	if b.Git("branch", "--show-current") != "topic" || b.Git("rev-parse", "--abbrev-ref", "topic@{upstream}") != "origin/topic" {
		t.Fatal("tracking branch not created")
	}
	// An existing local branch is reused instead of re-created.
	b.Git("switch", "-q", "main")
	if err := Checkout(bg, b.R, b.Dir, CheckoutTarget{Kind: "remote", Name: "origin/topic"}); err != nil {
		t.Fatal(err)
	}
	if b.Git("branch", "--show-current") != "topic" {
		t.Fatal("existing local branch not reused")
	}
	if err := Checkout(bg, b.R, b.Dir, CheckoutTarget{Kind: "commit", Name: h}); err != nil {
		t.Fatal(err)
	}
	if b.Git("branch", "--show-current") != "" {
		t.Fatal("not in detached HEAD")
	}
}

func TestCheckoutRemoteNameWithSlash(t *testing.T) {
	a := testutil.NewRepo(t)
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "team/up", bare)
	a.Git("push", "-q", "team/up", "main:topic")
	a.Git("fetch", "-q", "team/up")
	if err := Checkout(bg, a.R, a.Dir, CheckoutTarget{Kind: "remote", Name: "team/up/topic"}); err != nil {
		t.Fatal(err)
	}
	if a.Git("branch", "--show-current") != "topic" {
		t.Fatal("slash remote not handled")
	}
}

func TestCheckoutDirtyConflict(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	repo.Git("switch", "-q", "-c", "other")
	repo.Write("a.txt", "other\n")
	repo.CommitAll("other")
	repo.Git("switch", "-q", "main")
	repo.Write("a.txt", "dirty\n")
	err := Checkout(bg, repo.R, repo.Dir, CheckoutTarget{Kind: "local", Name: "other"})
	if gitexec.KindOf(err) != gitexec.KindDirtyWorktree {
		t.Fatalf("want DirtyWorktree, got %v", err)
	}
}

func TestTags(t *testing.T) {
	repo := testutil.NewRepo(t)
	h := repo.CommitAll("base")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "origin", bare)
	if err := CreateTag(bg, repo.R, repo.Dir, "v1", h, "release one\n\n# not a comment"); err != nil {
		t.Fatal(err)
	}
	if repo.Git("cat-file", "-t", "v1") != "tag" {
		t.Fatal("annotated tag expected")
	}
	if !strings.Contains(repo.Git("tag", "-l", "--format=%(contents)", "v1"), "# not a comment") {
		t.Fatal("tag message altered")
	}
	if err := CreateTag(bg, repo.R, repo.Dir, "light", h, ""); err != nil {
		t.Fatal(err)
	}
	if repo.Git("cat-file", "-t", "light") != "commit" {
		t.Fatal("lightweight tag expected")
	}
	if err := PushTag(bg, repo.R, repo.Dir, "origin", "v1"); err != nil {
		t.Fatal(err)
	}
	if repo.Git("--git-dir", bare, "tag", "--list") != "v1" {
		t.Fatal("tag not pushed")
	}
	if err := DeleteTag(bg, repo.R, repo.Dir, "light"); err != nil {
		t.Fatal(err)
	}
	if repo.Git("tag", "--list") != "v1" {
		t.Fatal("wrong tag list")
	}
}

func TestRemoteBranchAndUpstream(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("base")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "origin", bare)
	repo.Git("push", "-q", "origin", "main", "main:gone")
	repo.Git("fetch", "-q", "origin")
	if err := SetUpstream(bg, repo.R, repo.Dir, "main", "origin/main"); err != nil {
		t.Fatal(err)
	}
	if repo.Git("rev-parse", "--abbrev-ref", "main@{upstream}") != "origin/main" {
		t.Fatal("upstream not set")
	}
	if err := DeleteRemoteBranch(bg, repo.R, repo.Dir, "origin", "gone"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(repo.Git("--git-dir", bare, "branch", "--list"), "gone") {
		t.Fatal("remote branch not deleted")
	}
}

func TestNamesAreNeverOptions(t *testing.T) {
	repo := testutil.NewRepo(t)
	h := repo.CommitAll("base")
	for _, bad := range []string{"-x", "--detach", "@{-1}"} {
		if CreateBranch(bg, repo.R, repo.Dir, bad, h, false) == nil {
			t.Errorf("CreateBranch accepted %q", bad)
		}
		if CreateTag(bg, repo.R, repo.Dir, bad, h, "") == nil && bad != "@{-1}" {
			t.Errorf("CreateTag accepted %q", bad)
		}
	}
	if CreateBranch(bg, repo.R, repo.Dir, "ok", "--orphan", false) == nil {
		t.Error("option-like start accepted")
	}
	if Checkout(bg, repo.R, repo.Dir, CheckoutTarget{Kind: "commit", Name: "--orphan=x"}) == nil {
		t.Error("option-like commit accepted")
	}
	if Checkout(bg, repo.R, repo.Dir, CheckoutTarget{Kind: "local", Name: "-c"}) == nil {
		t.Error("option-like local accepted")
	}
	if PushTag(bg, repo.R, repo.Dir, "--mirror", "v1") == nil {
		t.Error("option-like remote accepted")
	}
	if repo.Git("branch", "--show-current") != "main" {
		t.Fatal("repo state changed")
	}
}
