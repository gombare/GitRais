package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gitrais/internal/testutil"
)

func TestWorktreePath(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("dir/a.txt", "a\n")
	root, err := filepath.EvalSymlinks(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo.Dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir/a.txt", filepath.Join(repo.Dir, "filelink")); err != nil {
		t.Fatal(err)
	}

	ok := []struct{ rel, want string }{
		{"dir/a.txt", filepath.Join(root, "dir", "a.txt")},
		{"dir", filepath.Join(root, "dir")},
		// A deleted file: its nearest existing ancestor inside the repository.
		{"dir/gone.txt", filepath.Join(root, "dir")},
		{"missing/deep/x.txt", root},
		// A symlink as the last component is the entry itself (never followed).
		{"filelink", filepath.Join(root, "filelink")},
	}
	for _, c := range ok {
		got, err := WorktreePath(repo.Dir, c.rel)
		if err != nil || got != c.want {
			t.Errorf("%q: got %q %v, want %q", c.rel, got, err, c.want)
		}
	}
	bad := []string{"", "/etc/passwd", "../x", "dir/../../x", "./dir", "dir//a.txt", ".git/config", "dir/.GIT/x",
		"a\x00b", "link/secret", "link/../x"}
	for _, rel := range bad {
		if got, err := WorktreePath(repo.Dir, rel); !errors.Is(err, ErrInvalidFilePath) {
			t.Errorf("%q: got %q %v, want ErrInvalidFilePath", rel, got, err)
		}
	}
}
