package watch

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSource delivers batches sent on ch. Batches passed to hold model changes the
// OS has seen but not delivered yet (FSEvents latency): Flush, or the test via
// deliverLate, sends them.
type fakeSource struct {
	ch      chan []string
	once    sync.Once
	mu      sync.Mutex
	late    [][]string
	flushes atomic.Int32
}

func (f *fakeSource) Events() <-chan []string { return f.ch }
func (f *fakeSource) Close() error            { f.once.Do(func() { close(f.ch) }); return nil }
func (f *fakeSource) Flush()                  { f.flushes.Add(1); f.deliverLate() }

func (f *fakeSource) hold(batch []string) {
	f.mu.Lock()
	f.late = append(f.late, batch)
	f.mu.Unlock()
}

func (f *fakeSource) deliverLate() {
	f.mu.Lock()
	late := f.late
	f.late = nil
	f.mu.Unlock()
	for _, b := range late {
		f.ch <- b // unbuffered: received by the watcher loop when this returns
	}
}

func collect() (func(Kind), func() []Kind) {
	var mu sync.Mutex
	var got []Kind
	return func(k Kind) { mu.Lock(); got = append(got, k); mu.Unlock() },
		func() []Kind { mu.Lock(); defer mu.Unlock(); return append([]Kind(nil), got...) }
}

func TestDebounceMerges(t *testing.T) {
	src := &fakeSource{ch: make(chan []string)}
	emit, got := collect()
	w := New("/r", "/r/.git", src, 50*time.Millisecond, nil, emit)
	defer w.Close()
	src.ch <- []string{"/r/a.txt"}
	src.ch <- []string{"/r/.git/refs/heads/main"}
	time.Sleep(150 * time.Millisecond)
	if g := got(); len(g) != 1 || g[0] != WIP|Refs {
		t.Fatalf("got %v", g)
	}
}

func TestSuspendDefersEvents(t *testing.T) {
	src := &fakeSource{ch: make(chan []string)}
	emit, got := collect()
	w := New("/r", "/r/.git", src, 20*time.Millisecond, nil, emit)
	defer w.Close()
	resume := w.Suspend()
	src.ch <- []string{"/r/a.txt"}
	time.Sleep(80 * time.Millisecond)
	if len(got()) != 0 {
		t.Fatal("event emitted while suspended")
	}
	resume(true)
	time.Sleep(80 * time.Millisecond)
	if g := got(); len(g) != 1 || g[0] != WIP {
		t.Fatalf("got %v", g)
	}
	resume2 := w.Suspend()
	src.ch <- []string{"/r/b.txt"}
	time.Sleep(30 * time.Millisecond)
	resume2(false)
	time.Sleep(80 * time.Millisecond)
	if g := got(); len(g) != 1 {
		t.Fatalf("events not discarded: %v", g)
	}
}

// settle gives the watcher goroutine time to process a just-sent batch (the
// unbuffered send only guarantees it was received, not classified).
func settle() { time.Sleep(10 * time.Millisecond) }

func newTestWatcher(debounce time.Duration) (*Watcher, *fakeSource, func() []Kind) {
	src := &fakeSource{ch: make(chan []string)}
	emit, got := collect()
	return New("/r", "/r/.git", src, debounce, nil, emit), src, got
}

func TestSuspendNested(t *testing.T) {
	w, src, got := newTestWatcher(20 * time.Millisecond)
	defer w.Close()
	a := w.Suspend()
	b := w.Suspend()
	src.ch <- []string{"/r/a.txt"}
	settle()
	a(true)
	time.Sleep(80 * time.Millisecond)
	if len(got()) != 0 {
		t.Fatal("must stay suspended until the last resume")
	}
	b(true)
	time.Sleep(80 * time.Millisecond)
	if g := got(); len(g) != 1 || g[0] != WIP {
		t.Fatalf("got %v", g)
	}
}

func TestResumeIsIdempotent(t *testing.T) {
	w, src, got := newTestWatcher(20 * time.Millisecond)
	defer w.Close()
	a := w.Suspend()
	b := w.Suspend()
	a(true)
	a(true) // must not release b's suspension
	src.ch <- []string{"/r/a.txt"}
	settle()
	time.Sleep(80 * time.Millisecond)
	if len(got()) != 0 {
		t.Fatal("double resume released another suspension")
	}
	b(true)
	time.Sleep(80 * time.Millisecond)
	if len(got()) != 1 {
		t.Fatalf("got %v", got())
	}
}

func TestDiscardOnlyEventsDuringSuspension(t *testing.T) {
	w, src, got := newTestWatcher(40 * time.Millisecond)
	defer w.Close()

	// An event from before the suspension survives resume(false).
	src.ch <- []string{"/r/before.txt"}
	time.Sleep(5 * time.Millisecond) // reaches pending, debounce not yet elapsed
	resume := w.Suspend()
	src.ch <- []string{"/r/.git/refs/heads/x"} // arrives while suspended: dropped
	time.Sleep(10 * time.Millisecond)
	resume(false)
	time.Sleep(120 * time.Millisecond)
	if g := got(); len(g) != 1 || g[0] != WIP {
		t.Fatalf("want only the pre-suspension WIP, got %v", g)
	}

	// An event after resume is delivered.
	src.ch <- []string{"/r/.git/refs/heads/y"}
	time.Sleep(120 * time.Millisecond)
	if g := got(); len(g) != 2 || g[1] != Refs {
		t.Fatalf("post-resume event lost: %v", g)
	}
}

func TestDiscardOverlapping(t *testing.T) {
	w, src, got := newTestWatcher(20 * time.Millisecond)
	defer w.Close()
	a := w.Suspend()
	src.ch <- []string{"/r/a.txt"} // during A only
	settle()
	b := w.Suspend()
	src.ch <- []string{"/r/.git/refs/heads/x"} // during A and B
	settle()
	a(true)  // flush: keeps both while B is still active
	b(false) // drops what arrived since B started
	time.Sleep(80 * time.Millisecond)
	if g := got(); len(g) != 1 || g[0] != WIP {
		t.Fatalf("want WIP only, got %v", g)
	}
}

func TestCloseIdempotentAndNoEmitAfter(t *testing.T) {
	var calls atomic.Int32
	var closed atomic.Bool
	var late atomic.Bool
	src := &fakeSource{ch: make(chan []string)}
	w := New("/r", "/r/.git", src, time.Millisecond, nil, func(Kind) {
		if closed.Load() {
			late.Store(true)
		}
		calls.Add(1)
		time.Sleep(15 * time.Millisecond) // emit in flight while Close runs
	})
	src.ch <- []string{"/r/a.txt"}
	time.Sleep(8 * time.Millisecond) // emit is running now
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	closed.Store(true)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if late.Load() {
		t.Fatal("emit called after Close returned")
	}
	// Suspend/resume after Close must not schedule anything.
	w.Suspend()(true)
	time.Sleep(20 * time.Millisecond)
	if calls.Load() > 1 {
		t.Fatalf("emit calls = %d", calls.Load())
	}
}

func TestCloseDropsPendingEvents(t *testing.T) {
	w, src, got := newTestWatcher(200 * time.Millisecond)
	src.ch <- []string{"/r/a.txt"}
	time.Sleep(10 * time.Millisecond)
	w.Close()
	time.Sleep(300 * time.Millisecond)
	if len(got()) != 0 {
		t.Fatal("emit after Close")
	}
}

func TestConcurrentSuspendAndEvents(t *testing.T) {
	w, src, got := newTestWatcher(5 * time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r := w.Suspend()
				time.Sleep(time.Millisecond)
				r(j%2 == 0)
			}
		}(i)
	}
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case src.ch <- []string{"/r/a.txt"}:
				time.Sleep(time.Millisecond)
			}
		}
	}()
	wg.Wait()
	close(stop)
	// Everything is resumed: new events must still be delivered.
	before := len(got())
	time.Sleep(30 * time.Millisecond)
	src.ch <- []string{"/r/.git/refs/heads/z"}
	time.Sleep(60 * time.Millisecond)
	if len(got()) <= before {
		t.Fatal("watcher stuck suspended")
	}
	w.Close()
}

func TestResumeDiscardWaitsForLateEvents(t *testing.T) {
	w, src, got := newTestWatcher(20 * time.Millisecond)
	defer w.Close()
	resume := w.Suspend()
	src.hold([]string{"/r/.git/refs/heads/own"}) // the app's own write, still in flight
	resume(false)
	src.deliverLate() // what the OS would deliver later anyway (no-op after a Flush)
	time.Sleep(80 * time.Millisecond)
	if g := got(); len(g) != 0 {
		t.Fatalf("events produced while suspended leaked after resume(false): %v", g)
	}
	if src.flushes.Load() != 1 {
		t.Fatalf("resume(false) must flush the source once, got %d", src.flushes.Load())
	}
	src.ch <- []string{"/r/a.txt"}
	time.Sleep(80 * time.Millisecond)
	if g := got(); len(g) != 1 || g[0] != WIP {
		t.Fatalf("event after resume lost: %v", g)
	}
}

func TestResumeFlushDoesNotBarrier(t *testing.T) {
	w, src, _ := newTestWatcher(20 * time.Millisecond)
	defer w.Close()
	w.Suspend()(true)
	if src.flushes.Load() != 0 {
		t.Fatal("resume(true) needs no barrier")
	}
}

func TestResumeDiscardNoDeadlock(t *testing.T) {
	// From inside emit (timer goroutine) and after Close, resume(false) must return.
	src := &fakeSource{ch: make(chan []string)}
	var w *Watcher
	ready := make(chan struct{})
	returned := make(chan struct{}, 1)
	w = New("/r", "/r/.git", src, time.Millisecond, nil, func(Kind) {
		<-ready
		w.Suspend()(false)
		select {
		case returned <- struct{}{}:
		default:
		}
	})
	close(ready)
	src.ch <- []string{"/r/a.txt"}
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("resume(false) from emit deadlocked")
	}
	w.Close()
	done := make(chan struct{})
	go func() { w.Suspend()(false); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("resume(false) after Close deadlocked")
	}
}

func TestRescanSentinelForcesEverything(t *testing.T) {
	w, src, got := newTestWatcher(20 * time.Millisecond)
	defer w.Close()
	// A coalesced event on the git dir alone would classify as nothing useful;
	// the sentinel forces a full refresh.
	src.ch <- []string{"/r/.git/objects", rescanAll}
	time.Sleep(80 * time.Millisecond)
	if g := got(); len(g) != 1 || g[0] != WIP|Refs {
		t.Fatalf("got %v, want [WIP|Refs]", g)
	}
}

// resume(false) reports what it dropped, so the caller can still act on
// changes its own write did not cause (e.g. a fetch from a terminal).
func TestResumeReportsDroppedKinds(t *testing.T) {
	w, src, _ := newTestWatcher(20 * time.Millisecond)
	defer w.Close()
	resume := w.Suspend()
	src.ch <- []string{"/r/a.txt"}
	src.hold([]string{"/r/.git/refs/heads/x"}) // delivered by resume's flush
	if k := resume(false); k != WIP|Refs {
		t.Fatalf("dropped = %v, want WIP|Refs", k)
	}
	resume = w.Suspend()
	src.ch <- []string{"/r/b.txt"}
	time.Sleep(10 * time.Millisecond) // classified while still suspended
	if k := resume(true); k != 0 {
		t.Fatalf("resume(true) dropped %v", k)
	}
	time.Sleep(60 * time.Millisecond) // b.txt delivered
	if k := w.Suspend()(false); k != 0 {
		t.Fatalf("nothing happened, dropped = %v", k)
	}
}
