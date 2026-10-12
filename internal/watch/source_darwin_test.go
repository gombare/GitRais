//go:build darwin

package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fsnotify/fsevents"

	"gitrais/internal/testutil"
)

// sendTo returns a non-blocking emit: a full channel drops the notification
// instead of wedging the watcher (and Close).
func sendTo(ch chan Kind) func(Kind) {
	return func(k Kind) {
		select {
		case ch <- k:
		default:
		}
	}
}

// quiet drains ch until no notification arrives for d.
func quiet(ch chan Kind, d time.Duration) {
	for {
		select {
		case <-ch:
		case <-time.After(d):
			return
		}
	}
}

func TestFSEventsResumeDiscardsOwnWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("fsevents")
	}
	repo := testutil.NewRepo(t)
	repo.CommitAll("base")
	src, err := NewSource(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan Kind, 64)
	w := New(repo.Dir, filepath.Join(repo.Dir, ".git"), src, 50*time.Millisecond, nil, sendTo(ch))
	defer w.Close()
	waitFor(t, ch, WIP, func(i int) { repo.Write("x.txt", fmt.Sprintf("x%d\n", i)) })
	quiet(ch, 300*time.Millisecond)

	resume := w.Suspend()
	repo.Git("branch", "own-write")
	resume(false)
	select {
	case k := <-ch:
		t.Fatalf("the app's own write was reported after resume(false): %d", k)
	case <-time.After(500 * time.Millisecond):
	}

	repo.Git("branch", "after-resume")
	waitFor(t, ch, Refs, nil)
}

func TestFSEventsLinkedWorktreeGitDir(t *testing.T) {
	if testing.Short() {
		t.Skip("fsevents")
	}
	repo := testutil.NewRepo(t)
	repo.CommitAll("base")
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(parent, "wt")
	repo.Git("worktree", "add", "-q", "-b", "wt", wt)
	gitDir := filepath.Join(repo.Dir, ".git", "worktrees", "wt")
	if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); err != nil {
		t.Fatal(err)
	}
	src, err := NewSource(wt, gitDir)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan Kind, 64)
	w := New(wt, gitDir, src, 50*time.Millisecond, nil, sendTo(ch))
	defer w.Close()
	waitFor(t, ch, WIP, func(i int) {
		if err := os.WriteFile(filepath.Join(wt, "x.txt"), []byte(fmt.Sprint(i)), 0o644); err != nil {
			t.Error(err)
		}
	})
	quiet(ch, 300*time.Millisecond)
	// Only the worktree's own HEAD (in gitDir, outside the worktree) changes.
	repo.Git("-C", wt, "symbolic-ref", "HEAD", "refs/heads/elsewhere")
	waitFor(t, ch, Refs, nil)
}

func TestDedupeRoots(t *testing.T) {
	got := dedupeRoots([]string{"/r", "/r/.git", "", "/m/.git/worktrees/w", "/r/", "/m/.git/worktrees/w"})
	want := []string{"/r", "/m/.git/worktrees/w"}
	if !slices.Equal(got, want) {
		t.Fatalf("dedupeRoots = %v, want %v", got, want)
	}
}

func TestNewSourceNoPaths(t *testing.T) {
	if _, err := NewSource(); err == nil {
		t.Fatal("expected an error without paths")
	}
}

func TestConvertForceFlags(t *testing.T) {
	plain := convert([]fsevents.Event{{Path: "r/a.txt", Flags: fsevents.ItemModified}})
	if !slices.Equal(plain, []string{"/r/a.txt"}) {
		t.Fatalf("plain batch = %q", plain)
	}
	for _, f := range []fsevents.EventFlags{fsevents.MustScanSubDirs, fsevents.UserDropped,
		fsevents.KernelDropped, fsevents.RootChanged, fsevents.EventIDsWrapped, fsevents.HistoryDone} {
		got := convert([]fsevents.Event{{Path: "/r/.git", Flags: f}})
		if !slices.Contains(got, rescanAll) {
			t.Errorf("flag %#x: %q lacks the rescan sentinel", f, got)
		}
	}
	if got := convert([]fsevents.Event{{Path: "", Flags: fsevents.KernelDropped}}); !slices.Equal(got, []string{rescanAll}) {
		t.Errorf("pathless drop event = %q", got)
	}
}

func TestFSEventsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("fsevents")
	}
	repo := testutil.NewRepo(t) // Dir is already symlink-free (EvalSymlinks)
	repo.CommitAll("base")
	src, err := NewSource(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan Kind, 64)
	w := New(repo.Dir, filepath.Join(repo.Dir, ".git"), src, 100*time.Millisecond, nil, sendTo(ch))
	defer w.Close()

	// The stream starts asynchronously: keep touching files until the first event.
	waitFor(t, ch, WIP, func(i int) { repo.Write("x.txt", fmt.Sprintf("x%d\n", i)) })
	repo.Git("branch", "other")
	waitFor(t, ch, Refs, nil)
}

func TestFSEventsCloseIdempotent(t *testing.T) {
	src, err := NewSource(testutil.NewRepo(t).Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-src.Events(); ok {
		t.Fatal("Events must be closed after Close")
	}
	done := make(chan struct{})
	go func() { src.Flush(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Flush after Close must return")
	}
}

// waitFor waits up to 3s for an event containing want; poke (optional) is called
// every 250ms with the attempt number.
func waitFor(t *testing.T, ch chan Kind, want Kind, poke func(i int)) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	i := 0
	if poke != nil {
		poke(i)
	}
	for {
		select {
		case k := <-ch:
			if k&want != 0 {
				return
			}
		case <-tick.C:
			if poke != nil {
				i++
				poke(i)
			}
		case <-deadline:
			t.Fatalf("event %d not received within 3s", want)
		}
	}
}
