package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	s.armAutoFetch()
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
	s.armAutoFetch()
	s.SetAutoFetch(afTick)
	waitUntil(t, "pause", func() bool { return s.AutoFetchStatus().Reason == PauseAuth })
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
	s.armAutoFetch()
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
	s.armAutoFetch()
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
	s.armAutoFetch()
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

// countDone wraps backgroundFetch with fn, counting completed calls.
func countDone(t *testing.T, fn func(context.Context, *gitexec.Runner, string) error) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	orig := backgroundFetch
	backgroundFetch = func(ctx context.Context, r *gitexec.Runner, dir string) error {
		defer n.Add(1)
		return fn(ctx, r, dir)
	}
	t.Cleanup(func() { backgroundFetch = orig })
	return &n
}

// Fix round 1, item 1: a fetch that brings nothing must not notify nor reload;
// one that moves a remote ref notifies exactly once.
func TestAutoFetchNoopEmitsNothing(t *testing.T) {
	done := countDone(t, ops.FetchBackground)
	repo, push := remoteWithNewCommit(t)
	s, rec := open(t, repo)
	// The manual fetch arms the auto-fetch and creates origin/HEAD (a real
	// change, which the auto-fetch would otherwise report once).
	rec.waitFor(t, EventGraphProgress, func(d any) bool { return d.(GraphProgress).Done })
	loads := func() int {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		n := 0
		for i, e := range rec.events {
			if e == EventGraphProgress && rec.data[i].(GraphProgress).Done {
				n++
			}
		}
		return n
	}
	l0 := loads()
	if err := s.Fetch(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "graph reloaded after the fetch", func() bool { return loads() > l0 })
	time.Sleep(2 * watchDebounce) // let late watcher events settle
	refs0, resets0 := rec.count(EventRefsChanged), rec.count(EventGraphReset)
	// stop ends the ticks and waits past the watcher's debounce: while ticks
	// come faster than watchDebounce the watcher would coalesce their events.
	stop := func() {
		s.SetAutoFetch(0)
		waitUntil(t, "no auto-fetch running", func() bool { s.afMu.Lock(); defer s.afMu.Unlock(); return s.afCancel == nil })
		time.Sleep(3 * watchDebounce)
	}
	s.SetAutoFetch(afTick)
	waitUntil(t, "several no-op ticks", func() bool { return done.Load() >= 4 })
	stop()
	if n, m := rec.count(EventRefsChanged)-refs0, rec.count(EventGraphReset)-resets0; n != 0 || m != 0 {
		t.Fatalf("no-op auto-fetch emitted %d refs:changed and %d graph:reset", n, m)
	}
	push()
	n0 := done.Load()
	s.SetAutoFetch(afTick)
	waitUntil(t, "refs:changed after the remote moved", func() bool { return rec.count(EventRefsChanged) > refs0 })
	waitUntil(t, "more ticks", func() bool { return done.Load() >= n0+4 })
	stop()
	if n := rec.count(EventRefsChanged) - refs0; n != 1 {
		t.Fatalf("want exactly one refs:changed after the remote moved, got %d", n)
	}
	if m := rec.count(EventGraphReset) - resets0; m != 1 {
		t.Fatalf("want exactly one graph:reset after the remote moved, got %d", m)
	}
}

// A ref moved by someone else while the auto-fetch runs (watchers suspended)
// is still reported, by the before/after snapshot.
func TestAutoFetchKeepsExternalRefChange(t *testing.T) {
	repo, _ := remoteWithNewCommit(t)
	var once atomic.Bool
	countDone(t, func(ctx context.Context, r *gitexec.Runner, dir string) error {
		if once.CompareAndSwap(false, true) {
			repo.Git("branch", "external-during-fetch")
		}
		return nil
	})
	s, rec := open(t, repo)
	rec.waitFor(t, EventGraphProgress, func(d any) bool { return d.(GraphProgress).Done })
	time.Sleep(2 * watchDebounce)
	refs0 := rec.count(EventRefsChanged)
	s.armAutoFetch()
	s.SetAutoFetch(afTick)
	waitUntil(t, "refs:changed for the external branch", func() bool { return rec.count(EventRefsChanged) > refs0 })
}

// Fix round 1, item 2: no auto-fetch before a manual remote operation has
// succeeded in this run (any keychain dialog has then been answered).
func TestAutoFetchWaitsForManualRemoteOp(t *testing.T) {
	for _, op := range []string{"fetch", "pull", "push"} {
		t.Run(op, func(t *testing.T) {
			calls := countFetches(t, func(context.Context, *gitexec.Runner, string) error { return nil })
			repo, _ := remoteWithNewCommit(t)
			s, _ := open(t, repo)
			s.SetAutoFetch(afTick)
			if st := s.AutoFetchStatus(); !st.Enabled || !st.Paused || st.Reason != PauseWaitingManual {
				t.Fatalf("status = %+v", st)
			}
			time.Sleep(6 * afTick)
			if calls.Load() != 0 {
				t.Fatalf("auto-fetch ran %d times before any manual remote op", calls.Load())
			}
			var err error
			switch op {
			case "fetch":
				err = s.Fetch()
			case "pull":
				err = s.Pull(ops.PullFFOnly)
			case "push":
				err = s.PushCurrent(false)
			}
			if err != nil {
				t.Fatal(err)
			}
			if st := s.AutoFetchStatus(); st.Paused || st.Reason != "" {
				t.Fatalf("status after %s = %+v", op, st)
			}
			waitUntil(t, "auto-fetch after the manual op", func() bool { return calls.Load() > 0 })
		})
	}
}

func TestAutoFetchFailedManualOpDoesNotArm(t *testing.T) {
	calls := countFetches(t, func(context.Context, *gitexec.Runner, string) error { return nil })
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	repo.Git("remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	s, _ := open(t, repo)
	s.SetAutoFetch(afTick)
	if err := s.Fetch(); err == nil {
		t.Fatal("fetch from a missing remote should fail")
	}
	time.Sleep(6 * afTick)
	if calls.Load() != 0 || s.AutoFetchStatus().Reason != PauseWaitingManual {
		t.Fatalf("calls=%d status=%+v", calls.Load(), s.AutoFetchStatus())
	}
}

// Fix round 1, item 3: jittered intervals, staggered first tick.
func TestAutoFetchDelay(t *testing.T) {
	iv := 100 * time.Second
	cases := []struct {
		first bool
		r     float64
		want  time.Duration
	}{
		{true, 0, 0},
		{true, 0.25, 25 * time.Second},
		{true, 0.999, 99900 * time.Millisecond},
		{false, 0, 85 * time.Second},
		{false, 0.5, 100 * time.Second},
		{false, 1, 115 * time.Second},
	}
	for _, c := range cases {
		if got := autoFetchDelay(iv, c.first, c.r); got != c.want {
			t.Errorf("autoFetchDelay(%v, %v, %v) = %v, want %v", iv, c.first, c.r, got, c.want)
		}
	}
}

// The loop draws a random number for every scheduled tick: the first one
// staggered, the following ones jittered.
func TestAutoFetchUsesRand(t *testing.T) {
	var draws atomic.Int32
	orig := afRand
	afRand = func() float64 { draws.Add(1); return 0.5 }
	t.Cleanup(func() { afRand = orig })
	calls := countFetches(t, func(context.Context, *gitexec.Runner, string) error { return nil })
	repo, _ := remoteWithNewCommit(t)
	s, _ := open(t, repo)
	s.armAutoFetch()
	s.SetAutoFetch(afTick)
	waitUntil(t, "ticks", func() bool { return calls.Load() >= 3 })
	if d := draws.Load(); d < 3 {
		t.Fatalf("rand drawn %d times for %d ticks", d, calls.Load())
	}
}

// At most two background fetches run at once across all sessions.
func TestAutoFetchGlobalLimit(t *testing.T) {
	orig := autoFetchLimiter
	autoFetchLimiter = make(chan struct{}, 2)
	t.Cleanup(func() { autoFetchLimiter = orig })
	var cur, peak, total atomic.Int32
	release := make(chan struct{})
	countFetches(t, func(ctx context.Context, _ *gitexec.Runner, _ string) error {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		cur.Add(-1)
		total.Add(1)
		return nil
	})
	var sessions []*Session
	for range 4 {
		repo, _ := remoteWithNewCommit(t)
		s, _ := open(t, repo)
		sessions = append(sessions, s)
	}
	for _, s := range sessions {
		s.armAutoFetch()
		s.SetAutoFetch(afTick)
	}
	waitUntil(t, "two fetches running", func() bool { return cur.Load() == 2 })
	time.Sleep(6 * afTick)
	if p := peak.Load(); p != 2 {
		t.Fatalf("peak concurrent background fetches = %d, want 2", p)
	}
	close(release)
	waitUntil(t, "the others to run", func() bool { return total.Load() >= 4 })
	if p := peak.Load(); p > 2 {
		t.Fatalf("peak concurrent background fetches = %d", p)
	}
}

// Fix round 1, item 4: every state change is an autofetch:status event.
func TestAutoFetchStatusEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	repo.Git("remote", "add", "origin", testutil.NewBare(t, repo.R))
	s, rec := open(t, repo)
	statuses := func() []AutoFetchEvent {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		var out []AutoFetchEvent
		for i, e := range rec.events {
			if e == EventAutoFetchStatus {
				out = append(out, rec.data[i].(AutoFetchEvent))
			}
		}
		return out
	}
	last := func() AutoFetchEvent { st := statuses(); return st[len(st)-1] }

	s.SetAutoFetch(afTick)
	if got, want := last(), (AutoFetchEvent{Tab: "t1", Enabled: true, Paused: true, Reason: PauseWaitingManual}); got != want {
		t.Fatalf("enable: %+v, want %+v", got, want)
	}
	n := len(statuses())
	s.SetAutoFetch(2 * afTick) // same visible state: no event
	if len(statuses()) != n {
		t.Fatal("an unchanged state must not emit")
	}
	if err := s.Fetch(); err != nil {
		t.Fatal(err)
	}
	if got := last(); got.Paused || got.Reason != "" || !got.Enabled {
		t.Fatalf("armed: %+v", got)
	}
	repo.Git("remote", "set-url", "origin", srv.URL+"/repo.git")
	waitUntil(t, "auth pause event", func() bool { return last().Reason == PauseAuth })
	if !last().Paused {
		t.Fatalf("pause: %+v", last())
	}
	repo.Git("remote", "set-url", "origin", testutil.NewBare(t, repo.R))
	if err := s.Fetch(); err != nil {
		t.Fatal(err)
	}
	if got := last(); got.Paused || got.Reason != "" {
		t.Fatalf("resume: %+v", got)
	}
	s.SetAutoFetch(0)
	if got := last(); got.Enabled {
		t.Fatalf("disable: %+v", got)
	}
}

// armAutoFetch marks the session as if a manual remote operation had succeeded.
func (s *Session) armAutoFetch() { s.manualRemoteDone(nil, false, false) }

// UI wave 2A: the time of the last successful fetch (manual Fetch or Pull, or
// auto-fetch) is reported in the status and in autofetch:status events. A push
// does not fetch, so it does not count.
func TestAutoFetchLastFetch(t *testing.T) {
	var fail atomic.Bool
	calls := countFetches(t, func(context.Context, *gitexec.Runner, string) error {
		if fail.Load() {
			return errors.New("boom")
		}
		return nil
	})
	repo, _ := remoteWithNewCommit(t)
	s, rec := open(t, repo)
	if st := s.AutoFetchStatus(); st.LastFetch != 0 {
		t.Fatalf("no fetch yet: %+v", st)
	}
	if err := s.PushCurrent(false); err != nil {
		t.Fatal(err)
	}
	if st := s.AutoFetchStatus(); st.LastFetch != 0 {
		t.Fatalf("push is not a fetch: %+v", st)
	}
	before := time.Now().Unix()
	if err := s.Fetch(); err != nil {
		t.Fatal(err)
	}
	st := s.AutoFetchStatus()
	if st.LastFetch < before || st.LastFetch > time.Now().Unix() {
		t.Fatalf("lastFetch after Fetch = %d (before %d)", st.LastFetch, before)
	}
	lastEvent := func() AutoFetchEvent {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		for i := len(rec.events) - 1; i >= 0; i-- {
			if rec.events[i] == EventAutoFetchStatus {
				return rec.data[i].(AutoFetchEvent)
			}
		}
		return AutoFetchEvent{}
	}
	if ev := lastEvent(); ev.LastFetch != st.LastFetch {
		t.Fatalf("event %+v, status %+v", ev, st)
	}
	// An auto-fetch success updates it (and is reported) even when nothing moved.
	s.afMu.Lock()
	s.afLastFetch = 1
	s.afMu.Unlock()
	s.SetAutoFetch(afTick)
	waitUntil(t, "auto-fetch lastFetch", func() bool { return calls.Load() > 0 && s.AutoFetchStatus().LastFetch > 1 })
	waitUntil(t, "auto-fetch lastFetch event", func() bool { return lastEvent().LastFetch > 1 })
	// A failed fetch leaves it alone.
	s.SetAutoFetch(0)
	fail.Store(true)
	s.afMu.Lock()
	s.afLastFetch = 1
	s.afMu.Unlock()
	n := calls.Load()
	s.SetAutoFetch(afTick)
	waitUntil(t, "failed auto-fetch", func() bool { return calls.Load() > n })
	s.SetAutoFetch(0)
	if got := s.AutoFetchStatus().LastFetch; got != 1 {
		t.Fatalf("failed fetch changed lastFetch to %d", got)
	}
}

// A ref changed by someone else just before an auto-fetch tick must still be
// reported. Its file event may arrive only after the watchers were suspended
// (and be dropped with the fetch's own events) while the change is in both
// snapshots: the watchers are flushed before suspending, so the event is
// pending, not held, and is delivered after the fetch.
func TestAutoFetchKeepsRefChangeJustBeforeTick(t *testing.T) {
	countFetches(t, func(context.Context, *gitexec.Runner, string) error { return nil })
	origRand := afRand
	afRand = func() float64 { return 0.999 } // the loop's own first tick is an hour away
	t.Cleanup(func() { afRand = origRand })
	repo, _ := remoteWithNewCommit(t)
	s, rec := open(t, repo)
	rec.waitFor(t, EventGraphProgress, func(d any) bool { return d.(GraphProgress).Done })
	time.Sleep(2 * watchDebounce)
	s.armAutoFetch()
	s.SetAutoFetch(time.Hour)
	for i := range 4 {
		before := rec.count(EventRefsChanged)
		repo.Git("branch", fmt.Sprintf("external-%d", i))
		s.autoFetchTick()
		deadline := time.Now().Add(10 * watchDebounce)
		for rec.count(EventRefsChanged) == before {
			if time.Now().After(deadline) {
				t.Fatalf("round %d: the external branch was never reported", i)
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(2 * watchDebounce) // settle before the next round
	}
}
