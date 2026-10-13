package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"gitrais/internal/gitexec"
	"gitrais/internal/ops"
	"gitrais/internal/testutil"
)

const afTick = 30 * time.Millisecond

// countFetches wraps backgroundFetch, counting calls, for the test's duration.
func countFetches(t *testing.T, fn func(context.Context, *gitexec.Runner, string) error) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	orig := backgroundFetch
	backgroundFetch = func(ctx context.Context, r *gitexec.Runner, dir string) error {
		n.Add(1)
		return fn(ctx, r, dir)
	}
	t.Cleanup(func() { backgroundFetch = orig })
	return &n
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// remoteWithNewCommit returns a repo tracking a bare remote and a function
// that pushes a new commit to that remote from another clone.
func remoteWithNewCommit(t *testing.T) (*testutil.Repo, func() string) {
	t.Helper()
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "origin", bare)
	repo.Git("push", "-q", "-u", "origin", "main")
	return repo, func() string {
		other := testutil.NewRepoWith(t, repo.R)
		other.Git("pull", "-q", bare, "main")
		h := other.CommitAll("remote two")
		other.Git("push", "-q", bare, "HEAD:main")
		return h
	}
}

func TestAutoFetchTicksFetchAndEmitRefsChanged(t *testing.T) {
	calls := countFetches(t, ops.FetchBackground)
	repo, push := remoteWithNewCommit(t)
	s, rec := open(t, repo)
	if calls.Load() != 0 || s.AutoFetchStatus().Enabled {
		t.Fatal("auto-fetch must be off until configured (test default)")
	}
	h := push()
	before := rec.count(EventRefsChanged)
	s.SetAutoFetch(afTick)
	waitUntil(t, "origin/main fetched", func() bool {
		out, _ := repo.R.Run(bg, gitexec.Cmd{Dir: repo.Dir, Args: []string{"rev-parse", "origin/main"}})
		return string(out) == h+"\n"
	})
	waitUntil(t, "refs:changed", func() bool { return rec.count(EventRefsChanged) > before })
	if rec.count(EventOpProgress) != 0 {
		t.Fatal("auto-fetch must not emit op:progress")
	}
	if st := s.AutoFetchStatus(); !st.Enabled || st.Paused || st.IntervalSeconds != 0 {
		t.Fatalf("status = %+v", st)
	}
	// Disabling stops the ticks.
	s.SetAutoFetch(0)
	time.Sleep(2 * afTick)
	n := calls.Load()
	time.Sleep(5 * afTick)
	if calls.Load() != n {
		t.Fatal("auto-fetch kept running after being disabled")
	}
}

func TestAutoFetchAuthFailurePausesUntilManualFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	calls := countFetches(t, ops.FetchBackground)
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	repo.Git("remote", "add", "origin", srv.URL+"/repo.git")
	s, rec := open(t, repo)
	s.SetAutoFetch(afTick)
	waitUntil(t, "pause", func() bool { return s.AutoFetchStatus().Paused })
	if st := s.AutoFetchStatus(); st.Reason != PauseAuth {
		t.Fatalf("status = %+v", st)
	}
	n := calls.Load()
	time.Sleep(5 * afTick)
	if calls.Load() != n {
		t.Fatal("paused auto-fetch kept fetching")
	}
	if rec.count(EventOpProgress) != 0 {
		t.Fatal("auto-fetch must not emit op:progress")
	}
	// A failed manual fetch does not resume it; a successful one does.
	if err := s.Fetch(); err == nil {
		t.Fatal("manual fetch should fail too")
	}
	if !s.AutoFetchStatus().Paused {
		t.Fatal("a failed manual fetch must not resume auto-fetch")
	}
	repo.Git("remote", "set-url", "origin", testutil.NewBare(t, repo.R))
	if err := s.Fetch(); err != nil {
		t.Fatal(err)
	}
	if s.AutoFetchStatus().Paused {
		t.Fatal("a successful manual fetch must resume auto-fetch")
	}
	waitUntil(t, "auto-fetch resumed", func() bool { return calls.Load() > n })
}

// A user remote operation started during an auto-fetch cancels it instead of
// failing with ErrOpInProgress or waiting for it.
func TestAutoFetchYieldsToUserOps(t *testing.T) {
	started := make(chan struct{}, 10)
	var cancelled atomic.Int32
	countFetches(t, func(ctx context.Context, _ *gitexec.Runner, _ string) error {
		started <- struct{}{}
		<-ctx.Done()
		cancelled.Add(1)
		return ctx.Err()
	})
	repo, _ := remoteWithNewCommit(t)
	s, _ := open(t, repo)
	s.SetAutoFetch(afTick)
	<-started
	errc := make(chan error, 1)
	go func() { errc <- s.Fetch() }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("user fetch: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("user fetch blocked by the auto-fetch")
	}
	if cancelled.Load() == 0 {
		t.Fatal("the auto-fetch was not cancelled")
	}
	if s.AutoFetchStatus().Paused {
		t.Fatal("a yielded auto-fetch must not pause")
	}
	// Local writes yield too.
	<-started
	if err := s.Stage(bg, []string{"nothing-here"}); err != nil && errors.Is(err, ErrOpInProgress) {
		t.Fatal(err)
	}
	if cancelled.Load() < 2 {
		t.Fatal("a local write did not cancel the auto-fetch")
	}
}

// No tick fetches while a write is in progress (here: queued on the lock).
func TestAutoFetchSkipsDuringWrites(t *testing.T) {
	calls := countFetches(t, func(context.Context, *gitexec.Runner, string) error { return nil })
	repo, _ := remoteWithNewCommit(t)
	s, _ := open(t, repo)
	s.writeSem <- struct{}{}
	done := make(chan error, 1)
	go func() { done <- s.StageAll(bg) }()
	waitUntil(t, "write queued", func() bool { s.afMu.Lock(); defer s.afMu.Unlock(); return s.afWriters > 0 })
	s.SetAutoFetch(afTick)
	time.Sleep(6 * afTick)
	if calls.Load() != 0 {
		t.Fatalf("auto-fetch ran %d times during a write", calls.Load())
	}
	<-s.writeSem
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "auto-fetch after the write", func() bool { return calls.Load() > 0 })
}

func TestCloseStopsAutoFetch(t *testing.T) {
	base := runtime.NumGoroutine()
	started := make(chan struct{}, 10)
	calls := countFetches(t, func(ctx context.Context, _ *gitexec.Runner, _ string) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	repo, _ := remoteWithNewCommit(t)
	rec := newRecorder()
	s, err := Open("t1", repo.Dir, repo.R, rec.emit)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAutoFetch(afTick)
	<-started
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked on the auto-fetch")
	}
	n := calls.Load()
	time.Sleep(5 * afTick)
	if calls.Load() != n {
		t.Fatal("auto-fetch ran after Close")
	}
	waitUntil(t, "goroutines to exit", func() bool { return runtime.NumGoroutine() <= base })
}
