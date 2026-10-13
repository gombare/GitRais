package graph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gitrais/internal/testutil"
)

func TestLoadEmptyRepo(t *testing.T) {
	repo := testutil.NewRepo(t)
	idx := NewIndex()
	if err := Load(context.Background(), repo.R, repo.Dir, idx, LoadOptions{}); err != nil {
		t.Fatal(err)
	}
	if idx.Len() != 0 || !idx.Done() {
		t.Fatalf("len=%d done=%v", idx.Len(), idx.Done())
	}
}

func TestLoadMergeAndStash(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	base := repo.CommitAll("base")
	repo.Git("switch", "-q", "-c", "feature")
	repo.Write("f.txt", "f\n")
	feat := repo.CommitAll("feature work")
	repo.Git("switch", "-q", "main")
	repo.Write("a.txt", "2\n")
	mainC := repo.CommitAll("main work")
	repo.Git("merge", "-q", "--no-ff", "feature", "-m", "merge feature")
	merge := repo.Git("rev-parse", "HEAD")
	repo.Write("a.txt", "dirty\n")
	repo.Git("stash", "push", "-q", "-m", "wip")
	stashHash := repo.Git("rev-parse", "stash@{0}")

	idx := NewIndex()
	var progress []int
	err := Load(context.Background(), repo.R, repo.Dir, idx, LoadOptions{
		Stashes:    []Stash{{Hash: stashHash, Base: merge, Ref: "stash@{0}", Subject: "On main: wip"}},
		Batch:      2,
		OnProgress: func(n int) { progress = append(progress, n) },
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := idx.Rows(0, 100)
	var got []string
	for _, r := range rows {
		got = append(got, r.Hash[:7])
	}
	want := []string{stashHash[:7], merge[:7]}
	if got[0] != want[0] || got[1] != want[1] || rows[0].Kind != RowStash || rows[0].StashRef != "stash@{0}" {
		t.Fatalf("first rows = %v, want stash then merge", got)
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %d (%v), want 5", len(rows), got)
	}
	pos := map[string]int{}
	for i, r := range rows {
		pos[r.Hash] = i
	}
	if !(pos[merge] < pos[mainC] && pos[merge] < pos[feat] && pos[feat] < pos[base] && pos[mainC] < pos[base]) {
		t.Fatalf("non-topological order: %v", got)
	}
	if rows[pos[merge]].Subject != "merge feature" || rows[pos[merge]].Author != "Test" {
		t.Fatalf("metadata: %+v", rows[pos[merge]])
	}
	if len(progress) < 2 || progress[len(progress)-1] != 5 {
		t.Fatalf("progress = %v", progress)
	}
}

func TestLoadCancel(t *testing.T) {
	repo := testutil.NewRepo(t)
	for i := 0; i < 50; i++ {
		repo.CommitAll("c" + strings.Repeat("x", i%3))
	}
	ctx, cancel := context.WithCancel(context.Background())
	idx := NewIndex()
	err := Load(ctx, repo.R, repo.Dir, idx, LoadOptions{Batch: 10, OnProgress: func(int) { cancel() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want Canceled, got %v", err)
	}
	if !idx.Done() || !errors.Is(idx.Err(), context.Canceled) {
		t.Fatal("index not closed with error")
	}
}

func TestLoadSeparatorsInText(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	subj := "hello\x1eworld\x1fx"
	repo.Git("commit", "--allow-empty", "-m", subj)
	repo.Git("commit", "--allow-empty", "-m", "after")
	idx := NewIndex()
	if err := Load(context.Background(), repo.R, repo.Dir, idx, LoadOptions{}); err != nil {
		t.Fatal(err)
	}
	rows := idx.Rows(0, 10)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[1].Subject != subj || rows[1].Author != "Test" {
		t.Fatalf("subject = %q author = %q", rows[1].Subject, rows[1].Author)
	}
}

func TestLoadStashWithUnreachableBase(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("base")
	repo.Git("switch", "-q", "-c", "tmp")
	repo.Write("b.txt", "b\n")
	tmpC := repo.CommitAll("on tmp")
	repo.Write("b.txt", "dirty\n")
	repo.Git("stash", "push", "-q", "-m", "wip")
	stashHash := repo.Git("rev-parse", "stash@{0}")
	repo.Git("switch", "-q", "main")
	repo.Git("branch", "-q", "-D", "tmp")

	idx := NewIndex()
	err := Load(context.Background(), repo.R, repo.Dir, idx, LoadOptions{
		Stashes: []Stash{{Hash: stashHash, Base: tmpC, Ref: "stash@{0}", Subject: "On tmp: wip"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if i, ok := idx.Find(stashHash); !ok || idx.Rows(i, 1)[0].Kind != RowStash {
		t.Fatalf("stash row missing (len=%d)", idx.Len())
	}
	if _, ok := idx.Find(tmpC); !ok {
		t.Fatal("base row missing")
	}
}

func TestLoadProgressNoDuplicates(t *testing.T) {
	repo := testutil.NewRepo(t)
	for i := 0; i < 4; i++ {
		repo.Git("commit", "--allow-empty", "-m", "c")
	}
	idx := NewIndex()
	var progress []int
	err := Load(context.Background(), repo.R, repo.Dir, idx, LoadOptions{
		Batch: 2, OnProgress: func(n int) { progress = append(progress, n) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) != 2 || progress[0] != 2 || progress[1] != 4 {
		t.Fatalf("progress = %v, want [2 4]", progress)
	}
}

// log.showSignature must not leak signature verification lines into the
// parsed records.
func TestLoadSignedCommitsWithShowSignature(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.SignWithSSH()
	a := repo.CommitAll("signed a")
	b := repo.CommitAll("signed b")
	if out := repo.Git("log", "-1", "--format=%H"); !strings.Contains(out, "signature") {
		t.Fatalf("log.showSignature not effective, log output: %q", out)
	}
	idx := NewIndex()
	if err := Load(context.Background(), repo.R, repo.Dir, idx, LoadOptions{}); err != nil {
		t.Fatal(err)
	}
	rows := idx.Rows(0, 10)
	if len(rows) != 2 || rows[0].Hash != b || rows[1].Hash != a || rows[0].Subject != "signed b" {
		t.Fatalf("rows = %+v", rows)
	}
}
