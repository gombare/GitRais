package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitrais/internal/testutil"
)

func TestFileVersions(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "v1\n")
	c1 := repo.CommitAll("one")
	repo.Write("a.txt", "v2\n")
	c2 := repo.CommitAll("two")
	repo.Write("a.txt", "v3\n")
	repo.Git("add", "a.txt")
	repo.Write("a.txt", "v4\n")
	repo.Write("n.txt", "new\n")
	repo.Write("b.bin", "\x00\x01")

	cases := []struct {
		req      VersionsRequest
		old, new string
	}{
		{VersionsRequest{Path: "a.txt"}, "v3\n", "v4\n"},
		{VersionsRequest{Path: "a.txt", Staged: true}, "v2\n", "v3\n"},
		{VersionsRequest{Path: "n.txt", Untracked: true}, "", "new\n"},
		{VersionsRequest{Path: "a.txt", Commit: c2}, "v1\n", "v2\n"},
		{VersionsRequest{Path: "a.txt", Commit: c1}, "", "v1\n"},
		// Missing objects/files read as empty.
		{VersionsRequest{Path: "nope.txt", Staged: true}, "", ""},
		{VersionsRequest{Path: "nope.txt", Commit: c2}, "", ""},
		{VersionsRequest{Path: "nope.txt"}, "", ""},
	}
	for _, c := range cases {
		v, err := FileVersions(bg, repo.R, repo.Dir, c.req)
		if err != nil || v.Old != c.old || v.New != c.new || v.Binary || v.TooLarge {
			t.Errorf("%+v: got %q→%q %+v %v; want %q→%q", c.req, v.Old, v.New, v, err, c.old, c.new)
		}
	}
	v, err := FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "b.bin", Untracked: true})
	if err != nil || !v.Binary || v.New != "" || v.Old != "" {
		t.Fatalf("binary = %+v %v", v, err)
	}
}

func TestFileVersionsRename(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("old.txt", "one\ntwo\nthree\nfour\n")
	repo.CommitAll("one")
	repo.Git("mv", "old.txt", "new.txt")
	repo.Write("new.txt", "one\ntwo\nthree\nfour\nfive\n")
	repo.Git("add", "new.txt")

	// Staged rename: HEAD:oldPath → :path.
	v, err := FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "new.txt", OldPath: "old.txt", Staged: true})
	if err != nil || v.Old != "one\ntwo\nthree\nfour\n" || v.New != "one\ntwo\nthree\nfour\nfive\n" {
		t.Fatalf("staged rename = %+v %v", v, err)
	}
	c := repo.CommitAll("rename")
	v, err = FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "new.txt", OldPath: "old.txt", Commit: c})
	if err != nil || v.Old != "one\ntwo\nthree\nfour\n" || v.New != "one\ntwo\nthree\nfour\nfive\n" {
		t.Fatalf("commit rename = %+v %v", v, err)
	}
}

func TestFileVersionsBinaryAndTooLarge(t *testing.T) {
	repo := testutil.NewRepo(t)
	// NUL after the first 8000 bytes: git treats the file as text.
	late := strings.Repeat("a", 8000) + "\x00"
	repo.Write("late.txt", late)
	repo.Write("bin.dat", "x\x00y")
	repo.CommitAll("one")
	repo.Write("bin.dat", "text now\n")
	repo.Write("big.txt", strings.Repeat("x", MaxDiffBytes+1))

	v, err := FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "late.txt"})
	if err != nil || v.Binary || v.Old != late || v.New != late {
		t.Fatalf("late NUL = binary %v, err %v", v.Binary, err)
	}
	// Binary on either side.
	v, err = FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "bin.dat"})
	if err != nil || !v.Binary || v.Old != "" || v.New != "" {
		t.Fatalf("old binary = %+v %v", v, err)
	}
	v, err = FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "big.txt", Untracked: true})
	if err != nil || !v.TooLarge || v.New != "" {
		t.Fatalf("too large worktree = tooLarge %v len %d %v", v.TooLarge, len(v.New), err)
	}
	repo.Git("add", "big.txt")
	v, err = FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "big.txt", Staged: true})
	if err != nil || !v.TooLarge || v.New != "" {
		t.Fatalf("too large blob = tooLarge %v len %d %v", v.TooLarge, len(v.New), err)
	}
}

func TestFileVersionsPathSafety(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "a\n")
	repo.Write("-n", "dash\n")
	repo.Write("1:x", "colon\n")
	repo.CommitAll("one")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), repo.Path("linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, repo.Path("link.txt")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"../x", "a/../../x", "/etc/passwd", "", "./a.txt", "a.txt\nHEAD:a.txt", "linkdir/secret.txt", "a\x00b", ".git/config", ".GIT/config", "sub/.Git/HEAD", ".git"} {
		if _, err := FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: p, Untracked: true}); err == nil {
			t.Errorf("path %q accepted", p)
		}
		if _, err := FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "a.txt", OldPath: p, Staged: true}); p != "" && !strings.HasPrefix(p, "linkdir") && err == nil {
			t.Errorf("oldPath %q accepted", p)
		}
	}
	// A symlink reads as its target path (what git diffs), never as the target's content.
	v, err := FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "link.txt", Untracked: true})
	if err != nil || v.New != outside {
		t.Fatalf("symlink = %+v %v", v, err)
	}
	// Option-like paths are plain paths.
	repo.Write("-n", "dash2\n")
	v, err = FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "-n"})
	if err != nil || v.Old != "dash\n" || v.New != "dash2\n" {
		t.Fatalf("option-like path = %+v %v", v, err)
	}
	// "1:x" is a path, not stage 1 of "x".
	repo.Write("1:x", "colon2\n")
	v, err = FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "1:x"})
	if err != nil || v.Old != "colon\n" || v.New != "colon2\n" {
		t.Fatalf("colon path = %+v %v", v, err)
	}
	// Commit must be a full hash.
	for _, c := range []string{"HEAD", "--output=x", "abc"} {
		if _, err := FileVersions(bg, repo.R, repo.Dir, VersionsRequest{Path: "a.txt", Commit: c}); err == nil {
			t.Errorf("commit %q accepted", c)
		}
	}
}
