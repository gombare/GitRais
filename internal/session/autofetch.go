package session

import (
	"bytes"
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"

	"gitrais/internal/gitexec"
	"gitrais/internal/ops"
	"gitrais/internal/watch"
)

// Periodic background fetch ("auto-fetch").
//
// Design: the auto-fetch never blocks the user. It runs outside the exclusive
// remote-operation slot and without the write lock, under its own context, and
// yields to every write: write (and so every remote operation) cancels a
// running auto-fetch and waits for its git process to exit before taking the
// lock, and a tick is skipped while any write or remote operation is in
// progress or queued. A cancelled auto-fetch is simply dropped until the next
// tick.
//
// It is silent when nothing changed: the fetch writes no FETCH_HEAD and runs
// no auto-maintenance, the watchers are suspended around it (their events are
// dropped), and refs:changed (with a graph reload) is emitted only when a
// snapshot of all refs and HEAD differs before and after the fetch. The
// snapshot also catches refs moved by someone else while the watchers were
// suspended; dropped working-tree events are forwarded as wip:changed.
//
// It starts only once a manual remote operation (Fetch, Pull, PullAutostash,
// PushCurrent) has succeeded in this session, so that any credential dialog
// (e.g. the macOS keychain) has been answered by the user in context and is
// never raised by a background process. Ticks are jittered (±autoFetchJitter)
// and the first one is delayed by a random fraction of the interval, so open
// tabs do not fetch in lockstep; at most maxConcurrentAutoFetches run at once
// in the whole app (autoFetchLimiter).
//
// It is never interactive (ops.FetchBackground): no askpass, no terminal
// prompt, ssh in BatchMode. An authentication or network failure (or a fetch
// that exceeds autoFetchTimeout) pauses it until the next successful manual
// Fetch. It emits no op:progress and its errors are never shown to the user;
// every visible state change is reported as an autofetch:status event.

// Pause reasons reported by AutoFetchStatus.
const (
	PauseAuth    = "auth"
	PauseNetwork = "network"
	// PauseWaitingManual: no manual remote operation has succeeded yet.
	PauseWaitingManual = "waiting-manual"
)

const (
	// autoFetchJitter is the relative random variation of each interval.
	autoFetchJitter = 0.15
	// maxConcurrentAutoFetches caps the background fetches running at once
	// across all sessions.
	maxConcurrentAutoFetches = 2
)

// Test seams.
var (
	backgroundFetch = ops.FetchBackground
	// defaultAutoFetchInterval is the interval a new Session starts with,
	// before the app applies the user's settings (0 = disabled).
	defaultAutoFetchInterval = 60 * time.Second
	autoFetchTimeout         = 2 * time.Minute
	// afRand returns a number in [0, 1) for jitter and stagger.
	afRand = rand.Float64
	// autoFetchLimiter is the app-wide semaphore of background fetches.
	autoFetchLimiter = make(chan struct{}, maxConcurrentAutoFetches)
)

// AutoFetchStatus is the auto-fetch state of a session.
type AutoFetchStatus struct {
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"intervalSeconds"`
	Paused          bool   `json:"paused"`
	Reason          string `json:"reason"` // PauseAuth | PauseNetwork | PauseWaitingManual | ""
}

// AutoFetchEvent is the payload of EventAutoFetchStatus.
type AutoFetchEvent struct {
	Tab     string `json:"tab"`
	Enabled bool   `json:"enabled"`
	Paused  bool   `json:"paused"`
	Reason  string `json:"reason"`
}

// autoFetchDelay is the wait before the next tick for interval iv and a random
// r in [0, 1): r·iv for the first tick (stagger), iv ± autoFetchJitter after.
func autoFetchDelay(iv time.Duration, first bool, r float64) time.Duration {
	if first {
		return time.Duration(math.Round(float64(iv) * r))
	}
	return time.Duration(math.Round(float64(iv) * (1 + autoFetchJitter*(2*r-1))))
}

// SetAutoFetch sets the auto-fetch interval; 0 disables it. The next tick is
// rescheduled (staggered, as a first tick).
func (s *Session) SetAutoFetch(interval time.Duration) {
	s.afMu.Lock()
	s.afInterval = max(interval, 0)
	s.afMu.Unlock()
	s.notifyAutoFetch()
	select {
	case s.afKick <- struct{}{}:
	default:
	}
}

func (s *Session) AutoFetchStatus() AutoFetchStatus {
	s.afMu.Lock()
	defer s.afMu.Unlock()
	return s.autoFetchStatusLocked()
}

func (s *Session) autoFetchStatusLocked() AutoFetchStatus {
	st := AutoFetchStatus{Enabled: s.afInterval > 0, IntervalSeconds: int(s.afInterval / time.Second),
		Paused: s.afPaused, Reason: s.afReason}
	if !s.afPaused && !s.afArmed {
		st.Paused, st.Reason = true, PauseWaitingManual
	}
	return st
}

// notifyAutoFetch emits autofetch:status when the visible state (enabled,
// paused, reason) differs from the last one emitted. afEmitMu keeps the
// events in state order.
func (s *Session) notifyAutoFetch() {
	s.afEmitMu.Lock()
	defer s.afEmitMu.Unlock()
	st := s.AutoFetchStatus()
	ev := AutoFetchEvent{Tab: s.ID, Enabled: st.Enabled, Paused: st.Paused, Reason: st.Reason}
	if s.afNotified && ev == s.afLast {
		return
	}
	s.afNotified, s.afLast = true, ev
	s.emit(EventAutoFetchStatus, ev)
}

// manualRemoteDone records the outcome of a manual remote operation: success
// arms the auto-fetch and, when resume is set (Fetch), clears a pause.
func (s *Session) manualRemoteDone(err error, resume bool) {
	if err != nil {
		return
	}
	s.afMu.Lock()
	s.afArmed = true
	if resume {
		s.afPaused, s.afReason = false, ""
	}
	s.afMu.Unlock()
	s.notifyAutoFetch()
}

// beginWrite registers a write and cancels a running auto-fetch, waiting for
// it to stop. endWrite must follow.
func (s *Session) beginWrite() {
	s.afMu.Lock()
	s.afWriters++
	cancel, done := s.afCancel, s.afDone
	s.afMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (s *Session) endWrite() {
	s.afMu.Lock()
	s.afWriters--
	s.afMu.Unlock()
}

// autoFetchLoop runs until Close. Ticks run on this goroutine, so two
// auto-fetches of a session never overlap.
func (s *Session) autoFetchLoop() {
	defer s.afWG.Done()
	var timer *time.Timer
	var tick <-chan time.Time
	reset := func(first bool) {
		if timer != nil {
			timer.Stop()
		}
		tick = nil
		s.afMu.Lock()
		iv := s.afInterval
		s.afMu.Unlock()
		if iv > 0 {
			timer = time.NewTimer(autoFetchDelay(iv, first, afRand()))
			tick = timer.C
		}
	}
	reset(true)
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.afKick:
			reset(true)
		case <-tick:
			s.autoFetchTick()
			reset(false)
		}
	}
}

func (s *Session) autoFetchTick() {
	s.afMu.Lock()
	if s.afInterval == 0 || !s.afArmed || s.afPaused || s.afWriters > 0 || s.opRunning() || s.ctx.Err() != nil {
		s.afMu.Unlock()
		return
	}
	ctx, yield := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.afCancel, s.afDone = yield, done
	s.afMu.Unlock()

	moved, err := s.autoFetch(ctx)

	s.afMu.Lock()
	s.afCancel, s.afDone = nil, nil
	paused := s.afPaused
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		// success, or yielded to a write / closed: nothing to record
	case errors.Is(err, context.DeadlineExceeded):
		s.afPaused, s.afReason = true, PauseNetwork
	case gitexec.KindOf(err) == gitexec.KindAuthFailed:
		s.afPaused, s.afReason = true, PauseAuth
	case gitexec.KindOf(err) == gitexec.KindNetwork:
		s.afPaused, s.afReason = true, PauseNetwork
	}
	changed := s.afPaused != paused
	s.afMu.Unlock()
	yield()
	close(done)
	if changed {
		s.notifyAutoFetch()
	}
	if moved {
		s.refsChanged()
	}
}

// autoFetch runs one background fetch under the app-wide limiter, with the
// watchers suspended, and reports whether any ref or HEAD moved meanwhile.
// ctx is the yield context (cancelled by a write or Close).
func (s *Session) autoFetch(ctx context.Context) (moved bool, err error) {
	lim := autoFetchLimiter
	select {
	case lim <- struct{}{}:
	case <-ctx.Done():
		return false, context.Canceled // waiting for a slot never pauses
	}
	defer func() { <-lim }()

	before, berr := s.refSnapshot(ctx)
	if ctx.Err() != nil {
		return false, context.Canceled
	}
	resumes := make([]func(bool) watch.Kind, 0, len(s.watchers))
	for _, w := range s.watchers {
		resumes = append(resumes, w.Suspend())
	}
	fctx, cancel := context.WithTimeout(ctx, autoFetchTimeout)
	err = backgroundFetch(fctx, s.r, s.Dir)
	cancel()
	// Resume (dropping the fetch's own events) before the second snapshot, so
	// a ref moved by someone else at any time is either in the snapshot or
	// reported by the watcher.
	var dropped watch.Kind
	for _, resume := range resumes {
		dropped |= resume(false)
	}
	after, aerr := s.refSnapshot(s.ctx)
	if berr == nil && aerr == nil {
		moved = !bytes.Equal(before, after)
	} else {
		moved = dropped&watch.Refs != 0 // no snapshot: trust the watcher
	}
	// A fetch never touches the working tree or the index: such events are
	// someone else's.
	if dropped&watch.WIP != 0 {
		s.emit(EventWipChanged, TabEvent{s.ID})
	}
	return moved, err
}

// refSnapshot lists all refs and HEAD (with its symbolic target).
func (s *Session) refSnapshot(ctx context.Context) ([]byte, error) {
	return s.r.Run(ctx, gitexec.Cmd{Dir: s.Dir, Args: []string{"for-each-ref", "--include-root-refs",
		"--format=%(refname) %(objectname) %(symref)"}, Env: gitexec.ReadOnlyEnv})
}
