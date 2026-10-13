package ops

import (
	"errors"
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
