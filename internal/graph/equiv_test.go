package graph

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gitrais/internal/genrepo"
	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

// referenceRows is the straightforward loader (every row stores its own
// segments, hashes as strings) that the compact Index must reproduce.
func referenceRows(t *testing.T, r *gitexec.Runner, dir string, stashes []Stash) []Row {
	t.Helper()
	ctx := context.Background()
	revs, err := startRevs(ctx, r, dir)
	if err != nil {
		t.Fatal(err)
	}
	byBase := map[string][]Stash{}
	for _, s := range stashes {
		if _, seen := byBase[s.Base]; !seen {
			revs = append(revs, s.Base)
		}
		byBase[s.Base] = append(byBase[s.Base], s)
	}
	var (
		layout Layout
		rows   []Row
	)
	place := func(row Row) {
		p := layout.Next(Commit{Hash: row.Hash, Parents: row.Parents})
		row.Lane, row.Color, row.Up, row.Down = p.Lane, p.Color, p.Up, p.Down
		rows = append(rows, row)
	}
	args := append([]string{"log", "-z", "--topo-order", "--no-color", logFormat}, append(revs, "--")...)
	err = r.Stream(ctx, gitexec.Cmd{Dir: dir, Args: args}, 0, func(rec []byte) error {
		if len(rec) == 0 {
			return nil
		}
		f := strings.SplitN(string(rec), "\x1f", 6)
		ts, err := strconv.ParseInt(f[4], 10, 64)
		if err != nil {
			return err
		}
		for _, s := range byBase[f[0]] {
			place(Row{Hash: s.Hash, Parents: []string{s.Base}, Kind: RowStash, StashRef: s.Ref,
				Subject: s.Subject, Author: s.Author, Email: s.Email, Time: s.Time})
		}
		place(Row{Hash: f[0], Parents: strings.Fields(f[1]), Author: f[2], Email: f[3], Time: ts, Subject: f[5]})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestIndexMatchesReference checks that Rows and Find on a loaded Index are
// identical to the reference loader on a synthetic repository with many
// lanes, merges and stashes, for windows crossing checkpoint and batch borders.
func TestIndexMatchesReference(t *testing.T) {
	n := 20_000
	if testing.Short() {
		n = 2_000
	}
	r := testutil.Runner(t)
	dir := filepath.Join(t.TempDir(), "repo")
	if err := genrepo.Create(context.Background(), r, dir, n, 12); err != nil {
		t.Fatal(err)
	}
	plain := referenceRows(t, r, dir, nil)
	rng := rand.New(rand.NewSource(7))
	fakeHash := func() string {
		var b strings.Builder
		for range 40 {
			fmt.Fprintf(&b, "%x", rng.Intn(16))
		}
		return b.String()
	}
	var stashes []Stash
	for i, pos := range []int{0, 1, 511, 512, 513, len(plain) / 2, len(plain) / 2, len(plain) - 1} {
		stashes = append(stashes, Stash{Hash: fakeHash(), Base: plain[pos].Hash, Ref: fmt.Sprintf("stash@{%d}", i),
			Subject: fmt.Sprintf("On b: wip %d", i), Author: "Stasher", Email: "s@example.com", Time: int64(1000 + i)})
	}
	want := referenceRows(t, r, dir, stashes)

	idx := NewIndex()
	if err := Load(context.Background(), r, dir, idx, LoadOptions{Stashes: stashes, Batch: 777}); err != nil {
		t.Fatal(err)
	}
	if idx.Len() != len(want) {
		t.Fatalf("Len = %d, want %d", idx.Len(), len(want))
	}
	check := func(start, count int) {
		t.Helper()
		got := idx.Rows(start, count)
		lo := min(max(start, 0), len(want))
		hi := min(max(lo+count, lo), len(want))
		if start >= len(want) || count <= 0 {
			lo, hi = 0, 0
		}
		if len(got) != hi-lo {
			t.Fatalf("Rows(%d,%d): %d rows, want %d", start, count, len(got), hi-lo)
		}
		for i := range got {
			if !reflect.DeepEqual(got[i], want[lo+i]) {
				t.Fatalf("Rows(%d,%d)[%d] (row %d):\n got %+v\nwant %+v", start, count, i, lo+i, got[i], want[lo+i])
			}
		}
	}
	check(0, len(want))
	check(-5, 10)
	check(len(want)-3, 100)
	check(len(want), 1)
	check(3, 0)
	for _, b := range []int{511, 512, 1023, 1024, 4095, 4096, 4097} {
		check(b-1, 3)
		check(b, 1)
	}
	for range 300 {
		check(rng.Intn(len(want)), 1+rng.Intn(700))
	}
	for i, row := range want {
		if j, ok := idx.Find(row.Hash); !ok || j != i {
			t.Fatalf("Find(%s) = %d %v, want %d", row.Hash, j, ok, i)
		}
	}
	for _, h := range []string{"", "zz", want[0].Hash[:7], strings.ToUpper(want[0].Hash), want[0].Hash + "00", fakeHash()} {
		if j, ok := idx.Find(h); ok {
			t.Fatalf("Find(%q) = %d, want not found", h, j)
		}
	}
}

// TestRowsDuringLoad reads windows and finds rows while the loader appends
// rows and checkpoints (run with -race).
func TestRowsDuringLoad(t *testing.T) {
	r := testutil.Runner(t)
	dir := filepath.Join(t.TempDir(), "repo")
	if err := genrepo.Create(context.Background(), r, dir, 3_000, 5); err != nil {
		t.Fatal(err)
	}
	idx := NewIndex()
	done := make(chan error, 1)
	go func() { done <- Load(context.Background(), r, dir, idx, LoadOptions{Batch: 100}) }()
	rng := rand.New(rand.NewSource(3))
	for !idx.Done() {
		if n := idx.Len(); n > 0 {
			rows := idx.Rows(rng.Intn(n), 50)
			if len(rows) == 0 || rows[0].Up == nil {
				t.Fatalf("Rows during load: %d rows, %+v", len(rows), rows)
			}
			if i, ok := idx.Find(rows[0].Hash); !ok || idx.Rows(i, 1)[0].Hash != rows[0].Hash {
				t.Fatal("Find during load")
			}
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if idx.Len() != 3_000 {
		t.Fatalf("Len = %d", idx.Len())
	}
}
