package ops

import (
	"errors"
	"strings"
	"testing"

	"gitrais/internal/testutil"
)

// Diffs only accept repository-relative paths: an untracked diff of
// "../../etc/hosts" would otherwise read a file outside the repository.
func TestDiffsRejectInvalidPaths(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "a\n")
	head := repo.CommitAll("one")
	bad := []string{"../../../../etc/hosts", "/etc/hosts", ".git/config", "a/../../x", ""}
	for _, p := range bad {
		for _, req := range []WipDiffRequest{{Path: p, Untracked: true}, {Path: p}, {Path: p, Staged: true}} {
			if _, err := WipDiff(bg, repo.R, repo.Dir, req); !errors.Is(err, ErrInvalidFilePath) {
				t.Errorf("WipDiff(%+v) err = %v", req, err)
			}
		}
		if _, err := CommitFileDiff(bg, repo.R, repo.Dir, head, head, p, ""); !errors.Is(err, ErrInvalidFilePath) {
			t.Errorf("CommitFileDiff(%q) err = %v", p, err)
		}
		if p != "" {
			if _, err := CommitFileDiff(bg, repo.R, repo.Dir, head, head, "a.txt", p); !errors.Is(err, ErrInvalidFilePath) {
				t.Errorf("CommitFileDiff(oldPath %q) err = %v", p, err)
			}
		}
	}
}

// A staged rename shows as R when the old path is passed along.
func TestWipDiffStagedRename(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("old.txt", strings.Repeat("line\n", 20))
	repo.CommitAll("one")
	repo.Git("mv", "old.txt", "new.txt")
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "new.txt", OldPath: "old.txt", Staged: true})
	if err != nil || fd.Status != "R" || fd.OldPath != "old.txt" || fd.NewPath != "new.txt" {
		t.Fatalf("staged rename = %+v, %v", fd, err)
	}
	if _, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "new.txt", OldPath: "../x", Staged: true}); !errors.Is(err, ErrInvalidFilePath) {
		t.Fatalf("invalid oldPath: err = %v", err)
	}
}
