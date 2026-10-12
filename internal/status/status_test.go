package status

import (
	"context"
	"testing"

	"gitrais/internal/testutil"
)

const sample = "# branch.oid abc\x00# branch.head main\x00# branch.upstream origin/main\x00# branch.ab +2 -1\x00" +
	"1 M. N... 100644 100644 100644 h1 h2 staged.txt\x00" +
	"1 .M N... 100644 100644 100644 h1 h1 dir/un staged.txt\x00" +
	"2 R. N... 100644 100644 100644 h1 h1 R100 new name.txt\x00old.txt\x00" +
	"u UU N... 100644 100644 100644 100644 h1 h2 h3 conf.txt\x00" +
	"? untracked file.txt\x00" +
	"! ignored.txt\x00"

func TestParse(t *testing.T) {
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if s.Branch != "main" || s.Upstream != "origin/main" || s.Ahead != 2 || s.Behind != 1 || s.Detached || s.Unborn {
		t.Fatalf("header = %+v", s)
	}
	if len(s.Files) != 5 {
		t.Fatalf("files = %+v", s.Files)
	}
	f := s.Files
	if f[0].Path != "staged.txt" || f[0].Index != "M" || f[0].WorkTree != "." {
		t.Fatalf("f0 %+v", f[0])
	}
	if f[1].Path != "dir/un staged.txt" || f[1].WorkTree != "M" {
		t.Fatalf("f1 %+v", f[1])
	}
	if f[2].Path != "new name.txt" || f[2].OrigPath != "old.txt" || f[2].Index != "R" {
		t.Fatalf("f2 %+v", f[2])
	}
	if !f[3].Conflicted || f[3].Path != "conf.txt" {
		t.Fatalf("f3 %+v", f[3])
	}
	if !f[4].Untracked || f[4].Path != "untracked file.txt" {
		t.Fatalf("f4 %+v", f[4])
	}
	if len(s.Staged()) != 2 || len(s.Unstaged()) != 3 {
		t.Fatalf("staged=%d unstaged=%d", len(s.Staged()), len(s.Unstaged()))
	}
}

func TestParseDetachedUnborn(t *testing.T) {
	s, _ := Parse([]byte("# branch.oid (initial)\x00# branch.head main\x00"))
	if !s.Unborn || s.Branch != "main" {
		t.Fatalf("%+v", s)
	}
	s, _ = Parse([]byte("# branch.oid abc\x00# branch.head (detached)\x00"))
	if !s.Detached || s.Branch != "" {
		t.Fatalf("%+v", s)
	}
}

func TestReadRealRepo(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("one")
	repo.Write("a.txt", "2\n")
	repo.Write("b.txt", "new\n")
	s, err := Read(context.Background(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Unstaged()) != 2 || len(s.Staged()) != 0 || s.Branch != "main" {
		t.Fatalf("%+v", s)
	}
}

func TestParseMalformed(t *testing.T) {
	for _, in := range []string{"1 M.\x00", "2 R. N... 1 1 1 h h R100 new\x00", "u UU x\x00", "1 X N... 1 1 1 h h p\x00", "? \x00"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestReadNewDirAndSpecialNames(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("one")
	repo.Write("newdir/x y.txt", "x\n")
	repo.Write("newdir/è\tü.txt", "x\n")
	s, err := Read(context.Background(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Files) != 2 || s.Files[0].Path != "newdir/x y.txt" && s.Files[1].Path != "newdir/x y.txt" {
		t.Fatalf("%+v", s.Files)
	}
}
