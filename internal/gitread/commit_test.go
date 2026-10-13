package gitread

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

func TestReadCommit(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", strings.Repeat("line\n", 20))
	repo.Write("keep.txt", "1\n2\n3\n")
	repo.Write("gone.txt", "bye\n")
	root := repo.CommitAll("root")

	repo.Git("mv", "a.txt", "renamed.txt")
	repo.Write("keep.txt", "1\nTWO\n3\n4\n")
	repo.Git("rm", "-q", "gone.txt")
	repo.Write("bin.dat", "\x00\x01\x02")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "summary line\n\nbody line 1\nbody line 2")
	head := repo.Git("rev-parse", "HEAD")

	d, err := ReadCommit(context.Background(), repo.R, repo.Dir, head)
	if err != nil {
		t.Fatal(err)
	}
	if d.Summary != "summary line" || d.Body != "body line 1\nbody line 2" || d.Author != "Test" ||
		len(d.Parents) != 1 || d.Parents[0] != root {
		t.Fatalf("detail = %+v", d)
	}
	by := map[string]FileChange{}
	for _, f := range d.Files {
		by[f.Path] = f
	}
	if f := by["renamed.txt"]; f.Status != "R" || f.OldPath != "a.txt" {
		t.Fatalf("rename = %+v", f)
	}
	if f := by["keep.txt"]; f.Status != "M" || f.Additions != 2 || f.Deletions != 1 {
		t.Fatalf("modify = %+v", f)
	}
	if f := by["gone.txt"]; f.Status != "D" || f.Deletions != 1 {
		t.Fatalf("delete = %+v", f)
	}
	if f := by["bin.dat"]; f.Status != "A" || !f.Binary {
		t.Fatalf("binary = %+v", f)
	}

	rd, err := ReadCommit(context.Background(), repo.R, repo.Dir, root)
	if err != nil || len(rd.Files) != 3 || len(rd.Parents) != 0 {
		t.Fatalf("root = %+v %v", rd, err)
	}
}

func TestChangedFilesUnusualPaths(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("base.txt", "x\n")
	first := repo.CommitAll("base")
	repo.Write("dir with space/né ü.txt", "a\nb\n")
	repo.Write("quote\"d.txt", "q\n")
	second := repo.CommitAll("odd")

	files, err := ChangedFiles(context.Background(), repo.R, repo.Dir, first, second)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]FileChange{}
	for _, f := range files {
		by[f.Path] = f
	}
	if f, ok := by["dir with space/né ü.txt"]; !ok || f.Additions != 2 || f.Status != "A" {
		t.Fatalf("files = %+v", files)
	}
	if _, ok := by["quote\"d.txt"]; !ok {
		t.Fatalf("files = %+v", files)
	}
}

func TestChangedFilesRejectsOptionRevs(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "a\n")
	repo.CommitAll("one")
	out := filepath.Join(t.TempDir(), "leak")
	if _, err := ChangedFiles(context.Background(), repo.R, repo.Dir, "--output="+out, "HEAD"); err == nil {
		t.Fatal("expected an error for an option-like rev")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("git wrote the --output file")
	}
}

// Repositories using git extensions that go-git does not know must still be
// readable: metadata comes from the bundled git.
func TestReadCommitWithExtensions(t *testing.T) {
	r := testutil.Runner(t)
	sparse := testutil.NewRepoWith(t, r)
	sparse.Write("x/a.txt", "a\n")
	sparse.Write("y/b.txt", "b\n")
	sparse.CommitAll("base")
	sparse.Git("sparse-checkout", "set", "x") // sets extensions.worktreeConfig=true
	if v := sparse.Git("config", "extensions.worktreeConfig"); v != "true" {
		t.Fatalf("extensions.worktreeConfig = %q", v)
	}

	shaDir := filepath.Join(t.TempDir(), "sha256")
	if _, err := r.Run(context.Background(), gitexec.Cmd{Args: []string{"init", "-q", "-b", "main", "--object-format=sha256", shaDir}}); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) string {
		out, err := r.Run(context.Background(), gitexec.Cmd{Dir: dir, Args: args})
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.WriteFile(filepath.Join(shaDir, "f.txt"), []byte("f\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(shaDir, "add", "-A")
	run(shaDir, "commit", "-q", "-m", "sha256 root")
	run(shaDir, "commit", "-q", "--allow-empty", "-m", "sha256 second\n\nthe body")

	for _, dir := range []string{sparse.Dir, shaDir} {
		head := run(dir, "rev-parse", "HEAD")
		d, err := ReadCommit(context.Background(), r, dir, head)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if d.Hash != head || d.Author != "Test" || d.AuthorEmail != "test@example.com" || d.CommitTime == 0 {
			t.Fatalf("%s: detail = %+v", dir, d)
		}
	}
	d, _ := ReadCommit(context.Background(), r, shaDir, run(shaDir, "rev-parse", "HEAD"))
	if d.Summary != "sha256 second" || d.Body != "the body" || len(d.Parents) != 1 || len(d.Parents[0]) != 64 {
		t.Fatalf("sha256 detail = %+v", d)
	}
}

func TestReadCommitAndStashesWithShowSignature(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.SignWithSSH()
	repo.Write("a.txt", "a\n")
	head := repo.CommitAll("signed")
	repo.Write("a.txt", "b\n")
	repo.Git("stash", "push", "-q", "-m", "wip")
	d, err := ReadCommit(context.Background(), repo.R, repo.Dir, head)
	if err != nil || d.Hash != head || d.Summary != "signed" {
		t.Fatalf("ReadCommit = %+v, %v", d, err)
	}
	st, err := ReadStashes(context.Background(), repo.R, repo.Dir)
	if err != nil || len(st) != 1 || st[0].Base != head {
		t.Fatalf("ReadStashes = %+v, %v", st, err)
	}
}
