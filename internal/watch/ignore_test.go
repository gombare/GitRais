package watch

import (
	"os"
	"testing"

	"gitrais/internal/testutil"
)

func TestIgnoredPaths(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "build/\n*.log\n")
	repo.Write("build/out.bin", "x")
	repo.Write("a.log", "x")
	repo.Write("src/main.go", "x")
	ign, err := IgnoredPaths(t.Context(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ign("build/out.bin") || !ign("build") || !ign("a.log") || ign("src/main.go") {
		t.Fatal("wrong ignore predicate")
	}
	if !ign("build/") || !ign("build/new/deeper/file") {
		t.Error("paths below an ignored directory must be ignored")
	}
	if ign("builder") || ign("buildx/out.bin") || ign("src") {
		t.Error("prefix of a name must not match")
	}
}

func TestIgnoredPathsRefreshOnGitignoreChange(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "*.log\n")
	repo.Write("a.log", "x")
	repo.Write("b.tmp", "x")
	ign, err := IgnoredPaths(t.Context(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if ign("b.tmp") {
		t.Fatal("b.tmp must not be ignored yet")
	}
	repo.Write(".gitignore", "*.log\n*.tmp\n")
	if ign(".gitignore") {
		t.Error(".gitignore itself must not be ignored")
	}
	if !ign("b.tmp") || !ign("a.log") {
		t.Error("snapshot not refreshed after .gitignore change")
	}
}

func TestIgnoredPathsKeepsSnapshotOnFailure(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "*.log\n")
	repo.Write("a.log", "x")
	ign, err := IgnoredPaths(t.Context(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo.Path(".git")); err != nil {
		t.Fatal(err)
	}
	ign(".gitignore") // refresh fails: git no longer finds a repository
	if !ign("a.log") {
		t.Error("previous snapshot must survive a failed refresh")
	}
}

func TestIgnoredPathsError(t *testing.T) {
	r := testutil.Runner(t)
	if _, err := IgnoredPaths(t.Context(), r, t.TempDir()); err == nil {
		t.Fatal("expected an error outside a repository")
	}
}
