//go:build darwin

package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsevents"
)

type fseSource struct {
	es        *fsevents.EventStream
	out       chan []string
	flushReq  chan chan struct{}
	done      chan struct{}
	finished  chan struct{}
	closeOnce sync.Once
	streamMu  sync.Mutex // guards es against a Flush racing with Stop
	stopped   bool

	cookieDir string        // where Flush writes its marker files
	cookieSeq atomic.Uint64 // marker file counter
	waitMu    sync.Mutex
	waiters   map[string]chan struct{} // marker path -> closed once forwarded
}

// cookiePrefix names Flush's marker files. Events for them are never forwarded.
const cookiePrefix = ".gitrais-flush-"

// flushTimeout bounds how long Flush waits for its marker event.
const flushTimeout = 2 * time.Second

// NewSource watches the given paths recursively with FSEvents. Paths must be real
// paths (filepath.EvalSymlinks): FSEvents reports resolved paths. Pass the
// worktree root and, when it lives outside the root (linked worktree, separate
// git dir), the git dir too. Paths nested in another given path are skipped.
func NewSource(paths ...string) (Source, error) {
	roots := dedupeRoots(paths)
	if len(roots) == 0 {
		return nil, errors.New("watch: no path to watch")
	}
	es := &fsevents.EventStream{
		Paths:   roots,
		Latency: 50 * time.Millisecond,
		Flags:   fsevents.FileEvents | fsevents.NoDefer | fsevents.WatchRoot,
		// Buffered so the C callback rarely blocks while we are busy or stopping.
		Events: make(chan []fsevents.Event, 64),
	}
	if err := es.Start(); err != nil {
		return nil, err
	}
	s := &fseSource{
		es: es,
		// Unbuffered: a completed send means the reader has received the batch,
		// which is what Flush promises.
		out:       make(chan []string),
		flushReq:  make(chan chan struct{}),
		done:      make(chan struct{}),
		finished:  make(chan struct{}),
		cookieDir: cookieDir(roots),
		waiters:   map[string]chan struct{}{},
	}
	go s.loop()
	return s, nil
}

// dedupeRoots drops empty paths, duplicates and paths inside another given path.
func dedupeRoots(paths []string) []string {
	var roots []string
	for i, p := range paths {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		nested := false
		for j, q := range paths {
			if q == "" || i == j {
				continue
			}
			q = filepath.Clean(q)
			if _, ok := under(q, p); ok && (p != q || j < i) {
				nested = true // inside q, or a duplicate of an earlier path
				break
			}
		}
		if !nested {
			roots = append(roots, p)
		}
	}
	return roots
}

// cookieDir picks where Flush markers go: a root's .git directory, or a root
// that is itself a git dir (contains HEAD). Markers never go into a worktree:
// with no git dir among the roots it returns "" and Flush falls back to
// FlushSync.
func cookieDir(roots []string) string {
	for _, r := range roots {
		if fi, err := os.Stat(filepath.Join(r, ".git")); err == nil && fi.IsDir() {
			return filepath.Join(r, ".git")
		}
	}
	for _, r := range roots {
		if fi, err := os.Stat(filepath.Join(r, "HEAD")); err == nil && fi.Mode().IsRegular() {
			return r
		}
	}
	return ""
}

// sameDir compares directories the way the filesystem does: FSEvents reports
// on-disk case, which may differ from the case of the path the repo was opened with.
func sameDir(a, b string) bool {
	if caseInsensitive {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// forceFlags mark events after which individual paths cannot be trusted: events
// were coalesced or dropped, or the watched tree itself moved or was remounted.
const forceFlags = fsevents.MustScanSubDirs | fsevents.UserDropped | fsevents.KernelDropped |
	fsevents.RootChanged | fsevents.EventIDsWrapped | fsevents.HistoryDone |
	fsevents.Mount | fsevents.Unmount

// convert maps an FSEvents batch to absolute paths, adding rescanAll when a flag
// says the batch does not describe every change.
func convert(msg []fsevents.Event) []string {
	paths := make([]string, 0, len(msg))
	rescan := false
	for _, e := range msg {
		if e.Flags&forceFlags != 0 {
			rescan = true
		}
		p := e.Path
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		paths = append(paths, p)
	}
	if rescan {
		paths = append(paths, rescanAll)
	}
	return paths
}

func (s *fseSource) loop() {
	defer close(s.finished)
	defer close(s.out)
	defer s.stop()
	for {
		select {
		case <-s.done:
			return
		case msg, ok := <-s.es.Events:
			if !ok || !s.forward(msg) {
				return
			}
		case ack := <-s.flushReq:
			// Flush has already pushed every pending callback into es.Events:
			// forward what is buffered there before acknowledging.
			for drained := false; !drained; {
				select {
				case msg := <-s.es.Events:
					if !s.forward(msg) {
						return
					}
				default:
					drained = true
				}
			}
			close(ack)
		}
	}
}

// forward sends one batch to the reader, minus Flush markers, then releases the
// Flush calls whose marker was in it. False when the source is closing.
func (s *fseSource) forward(msg []fsevents.Event) bool {
	all := convert(msg)
	paths := all[:0:0]
	var seen []string
	for _, p := range all {
		if p != rescanAll && s.cookieDir != "" && sameDir(filepath.Dir(p), s.cookieDir) && strings.HasPrefix(filepath.Base(p), cookiePrefix) {
			seen = append(seen, p)
			continue
		}
		paths = append(paths, p)
	}
	if len(paths) > 0 {
		select {
		case s.out <- paths:
		case <-s.done:
			return false
		}
	}
	if len(seen) > 0 {
		s.waitMu.Lock()
		for _, p := range seen {
			if ch, ok := s.waiters[filepath.Base(p)]; ok {
				close(ch)
				delete(s.waiters, filepath.Base(p))
			}
		}
		s.waitMu.Unlock()
	}
	return true
}

// stop halts the stream while draining Events: the FSEvents callback does a
// blocking send and would otherwise wedge Stop (or a concurrent FlushSync) or
// leak its thread.
func (s *fseSource) stop() {
	drained := make(chan struct{})
	go func() {
		for {
			select {
			case <-s.es.Events:
			case <-drained:
				return
			}
		}
	}()
	s.streamMu.Lock()
	s.es.Stop()
	s.stopped = true
	s.streamMu.Unlock()
	close(drained)
}

func (s *fseSource) Events() <-chan []string { return s.out }

// Flush returns once every change made before the call has been received from
// Events. FSEvents offers no such barrier (FlushSync does not cover events still
// travelling from the kernel), so Flush writes a marker file and waits for its
// event: FSEvents delivers in event-ID order, so everything earlier has been
// forwarded by then. If the marker cannot be written it falls back to FlushSync,
// and it gives up after flushTimeout. It returns early when the source is closed.
// It must not be called from the goroutine that reads Events.
func (s *fseSource) Flush() {
	if s.cookieDir == "" {
		s.flushSync()
		return
	}
	name := filepath.Join(s.cookieDir, fmt.Sprintf("%s%d-%d", cookiePrefix, os.Getpid(), s.cookieSeq.Add(1)))
	seen := make(chan struct{})
	s.waitMu.Lock()
	s.waiters[filepath.Base(name)] = seen
	s.waitMu.Unlock()
	defer func() {
		s.waitMu.Lock()
		delete(s.waiters, filepath.Base(name))
		s.waitMu.Unlock()
	}()

	if err := os.WriteFile(name, nil, 0o600); err != nil {
		s.flushSync()
		return
	}
	defer os.Remove(name)
	s.streamMu.Lock()
	if !s.stopped {
		s.es.Flush(false) // skip the latency wait if the marker is already queued
	}
	s.streamMu.Unlock()

	t := time.NewTimer(flushTimeout)
	defer t.Stop()
	select {
	case <-seen:
	case <-s.finished:
	case <-t.C:
	}
}

// flushSync is the best-effort fallback: deliver what FSEvents already holds and
// forward it before returning.
func (s *fseSource) flushSync() {
	s.streamMu.Lock()
	if s.stopped {
		s.streamMu.Unlock()
		return
	}
	// Synchronous: returns after the callbacks for pending events have run, so
	// their batches are in es.Events (or already forwarded).
	s.es.Flush(true)
	s.streamMu.Unlock()

	ack := make(chan struct{})
	select {
	case s.flushReq <- ack:
	case <-s.finished:
		return
	}
	select {
	case <-ack:
	case <-s.finished:
	}
}

// Close stops the stream and waits until its goroutine has exited. Idempotent.
func (s *fseSource) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	<-s.finished
	return nil
}
