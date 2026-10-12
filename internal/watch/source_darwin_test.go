//go:build darwin

package watch

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gitrais/internal/testutil"
)

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
	w := New(repo.Dir, filepath.Join(repo.Dir, ".git"), src, 100*time.Millisecond, nil, func(k Kind) { ch <- k })
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
