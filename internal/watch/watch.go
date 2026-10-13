package watch

import (
	"sync"
	"sync/atomic"
	"time"
)

// Source delivers batches of changed absolute paths.
type Source interface {
	// Events returns the batch channel; it is closed after Close.
	Events() <-chan []string
	// Flush returns once every change that happened before the call has been
	// received from Events by its reader (it does not wait for the reader to
	// process it), or once the source is closed. It is called from a goroutine
	// other than the reader's, which must keep reading meanwhile.
	Flush()
	// Close stops the source. It must be safe to call more than once.
	Close() error
}

// rescanAll is a sentinel "path" a Source puts in a batch when the batch does not
// describe every change (events coalesced or dropped, watched root moved): the
// Watcher then reports every Kind.
const rescanAll = "\x00rescan-all"

// Watcher turns raw path batches into debounced, merged Kind notifications.
//
// Suspend semantics: Suspend is a counter, so overlapping suspensions (two git
// writes in flight) are fine; events are held until the last suspension is
// released. Each resume(false) discards the events that arrived since its own
// Suspend call (including ones that arrived while other suspensions were still
// active), never events from before it nor from after it returned.
type Watcher struct {
	root, gitDir string
	src          Source
	debounce     time.Duration
	ignore       func(string) bool
	emit         func(Kind)

	mu        sync.Mutex
	pending   Kind        // events that are not held back by a suspension
	held      []heldEntry // events that arrived while suspended, grouped by epoch
	epoch     int         // bumped by every Suspend and resume
	suspended int
	gen       int // invalidates timers that were stopped too late
	timer     *time.Timer
	inflight  int
	idle      *sync.Cond

	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
	emitMu    sync.Mutex // serializes emit calls
	done      chan struct{}
	barrierCh chan chan struct{}
}

type heldEntry struct {
	epoch int
	kind  Kind
}

// New starts a Watcher that reads src. root and gitDir should be real paths.
//
// emit is called from a separate goroutine, never concurrently with itself and
// never after Close has returned. Close waits for an emit in progress, so emit
// must not call Close nor block on anything (a lock, a channel send) that a
// caller of Close may hold or wait on while Close runs.
//
// ignore is called on the watcher goroutine and must be fast. It is also told,
// as ".git/info/exclude", when the repository's info/exclude file changes, so a
// filter such as IgnoreFilter can refresh; a plain predicate just answers it.
func New(root, gitDir string, src Source, debounce time.Duration, ignore func(string) bool, emit func(Kind)) *Watcher {
	w := &Watcher{root: root, gitDir: gitDir, src: src, debounce: debounce, ignore: ignore, emit: emit,
		done: make(chan struct{}), barrierCh: make(chan chan struct{})}
	w.idle = sync.NewCond(&w.mu)
	go w.loop()
	return w
}

func (w *Watcher) loop() {
	defer close(w.done)
	events := w.src.Events()
	for {
		select {
		case paths, ok := <-events:
			if !ok {
				return
			}
			w.handle(paths)
		case ack := <-w.barrierCh:
			// Every batch received before this point has been handled.
			close(ack)
		}
	}
}

func (w *Watcher) handle(paths []string) {
	var k Kind
	for _, p := range paths {
		if p == rescanAll {
			k |= WIP | Refs
			continue
		}
		if w.ignore != nil {
			if rel, ok := under(w.gitDir, p); ok && rel == "info/exclude" {
				w.ignore(excludeRel)
			}
		}
		k |= Classify(w.root, w.gitDir, p, w.ignore)
	}
	if k != 0 {
		w.add(k)
	}
}

// barrier returns once every change that happened before the call has been
// classified (or the watcher is closed). It must not run on the loop goroutine.
func (w *Watcher) barrier() {
	w.src.Flush()
	ack := make(chan struct{})
	select {
	case w.barrierCh <- ack:
	case <-w.done:
		return
	}
	select {
	case <-ack:
	case <-w.done:
	}
}

func (w *Watcher) add(k Kind) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed.Load() {
		return
	}
	if w.suspended > 0 {
		if n := len(w.held); n > 0 && w.held[n-1].epoch == w.epoch {
			w.held[n-1].kind |= k
		} else {
			w.held = append(w.held, heldEntry{w.epoch, k})
		}
		return
	}
	w.pending |= k
	w.schedule()
}

// schedule (re)arms the debounce timer; w.mu must be held.
func (w *Watcher) schedule() {
	if w.timer != nil {
		w.timer.Stop()
	}
	w.gen++
	gen := w.gen
	w.timer = time.AfterFunc(w.debounce, func() { w.fire(gen) })
}

// cancelTimer invalidates any armed timer; w.mu must be held.
func (w *Watcher) cancelTimer() {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.gen++
}

func (w *Watcher) fire(gen int) {
	w.mu.Lock()
	if w.closed.Load() || gen != w.gen || w.suspended > 0 {
		w.mu.Unlock()
		return
	}
	k := w.pending
	w.pending = 0
	w.timer = nil
	if k == 0 {
		w.mu.Unlock()
		return
	}
	w.inflight++
	w.mu.Unlock()

	w.emitMu.Lock()
	if !w.closed.Load() {
		w.emit(k)
	}
	w.emitMu.Unlock()

	w.mu.Lock()
	w.inflight--
	w.idle.Broadcast()
	w.mu.Unlock()
}

// Suspend holds back notifications (typically while the app itself writes to the
// repository). The returned resume must be called once; further calls are no-ops
// and return 0. resume(true) delivers what was held, resume(false) drops the
// events observed since this Suspend call and returns their kinds (0 for
// resume(true)), so the caller can still act on changes it did not cause.
//
// resume(false) first flushes the source and waits until everything it held back
// has been classified (the OS delivers file events with some latency), so the
// app's own writes made while suspended are really discarded. It may therefore
// block briefly; it may be called from any goroutine, including from emit, but
// not from ignore.
func (w *Watcher) Suspend() (resume func(flush bool) Kind) {
	w.mu.Lock()
	w.suspended++
	w.epoch++
	start := w.epoch
	w.cancelTimer()
	w.mu.Unlock()

	var once sync.Once
	return func(flush bool) (dropped Kind) {
		once.Do(func() {
			if !flush && !w.closed.Load() {
				w.barrier() // still suspended: late events land in held and are dropped below
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			w.suspended--
			w.epoch++
			if !flush {
				kept := w.held[:0]
				for _, h := range w.held {
					if h.epoch < start {
						kept = append(kept, h)
					} else {
						dropped |= h.kind
					}
				}
				w.held = kept
			}
			if w.suspended > 0 {
				return
			}
			for _, h := range w.held {
				w.pending |= h.kind
			}
			w.held = nil
			if w.pending != 0 && !w.closed.Load() {
				w.schedule()
			}
		})
		return dropped
	}
}

// Close stops the source and the watcher. It waits for the internal goroutine and
// for an emit in progress; after it returns emit is never called again. Close is
// idempotent and returns the source's Close error on every call.
func (w *Watcher) Close() error {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed.Store(true)
		w.cancelTimer()
		w.mu.Unlock()

		w.closeErr = w.src.Close()
		<-w.done

		w.mu.Lock()
		for w.inflight > 0 {
			w.idle.Wait()
		}
		w.mu.Unlock()
	})
	return w.closeErr
}
