package session

import (
	"bytes"
	"context"
	"errors"
	"time"

	"gitrais/internal/gitexec"
	"gitrais/internal/ops"
)

// Periodic background fetch ("auto-fetch").
//
// Design: the auto-fetch never blocks the user. It runs outside the exclusive
// remote-operation slot and without the write lock, under its own context, and
// yields to every write: write (and so every remote operation) cancels a
// running auto-fetch and waits for its git process to exit before taking the
// lock, and a tick is skipped while any write or remote operation is in
// progress or queued. A cancelled auto-fetch is simply dropped until the next
// tick. Fetch only updates remote-tracking refs, tags and FETCH_HEAD, so it
// needs no watcher suspension; the session emits refs:changed (and reloads the
// graph) itself when the fetched refs moved.
//
// It is never interactive (ops.FetchBackground): no askpass, no terminal
// prompt, ssh in BatchMode. An authentication or network failure (or a fetch
// that exceeds autoFetchTimeout) pauses it until the next successful manual
// Fetch. It emits no op:progress and its errors are never shown to the user.

// Pause reasons reported by AutoFetchStatus.
const (
	PauseAuth    = "auth"
	PauseNetwork = "network"
)

// Test seams.
var (
	backgroundFetch = ops.FetchBackground
	// defaultAutoFetchInterval is the interval a new Session starts with,
	// before the app applies the user's settings (0 = disabled).
	defaultAutoFetchInterval = 60 * time.Second
	autoFetchTimeout         = 2 * time.Minute
)

// AutoFetchStatus is the auto-fetch state of a session.
type AutoFetchStatus struct {
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"intervalSeconds"`
	Paused          bool   `json:"paused"`
	Reason          string `json:"reason"` // PauseAuth | PauseNetwork | ""
}

// SetAutoFetch sets the auto-fetch interval; 0 disables it. The next tick is
// scheduled one full interval from now.
func (s *Session) SetAutoFetch(interval time.Duration) {
	s.afMu.Lock()
	s.afInterval = max(interval, 0)
	s.afMu.Unlock()
	select {
	case s.afKick <- struct{}{}:
	default:
	}
}

func (s *Session) AutoFetchStatus() AutoFetchStatus {
	s.afMu.Lock()
	defer s.afMu.Unlock()
	return AutoFetchStatus{Enabled: s.afInterval > 0, IntervalSeconds: int(s.afInterval / time.Second),
		Paused: s.afPaused, Reason: s.afReason}
}

// resumeAutoFetch clears a pause (after a successful manual fetch).
func (s *Session) resumeAutoFetch() {
	s.afMu.Lock()
	s.afPaused, s.afReason = false, ""
	s.afMu.Unlock()
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
// auto-fetches never overlap.
func (s *Session) autoFetchLoop() {
	defer s.afWG.Done()
	var timer *time.Timer
	var tick <-chan time.Time
	reset := func() {
		if timer != nil {
			timer.Stop()
		}
		tick = nil
		s.afMu.Lock()
		iv := s.afInterval
		s.afMu.Unlock()
		if iv > 0 {
			timer = time.NewTimer(iv)
			tick = timer.C
		}
	}
	reset()
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
			reset()
		case <-tick:
			s.autoFetchTick()
			reset()
		}
	}
}

func (s *Session) autoFetchTick() {
	s.afMu.Lock()
	if s.afInterval == 0 || s.afPaused || s.afWriters > 0 || s.opRunning() || s.ctx.Err() != nil {
		s.afMu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, autoFetchTimeout)
	defer cancel()
	ctx, yield := context.WithCancel(ctx)
	done := make(chan struct{})
	s.afCancel, s.afDone = yield, done
	s.afMu.Unlock()

	before := s.fetchedRefs(ctx)
	err := backgroundFetch(ctx, s.r, s.Dir)
	moved := err == nil && !bytes.Equal(before, s.fetchedRefs(ctx))

	s.afMu.Lock()
	s.afCancel, s.afDone = nil, nil
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
	s.afMu.Unlock()
	yield()
	close(done)
	if moved {
		s.refsChanged()
	}
}

// fetchedRefs is a snapshot of the refs a fetch can move (nil on error, so a
// failed read counts as a change).
func (s *Session) fetchedRefs(ctx context.Context) []byte {
	out, err := s.r.Run(ctx, gitexec.Cmd{Dir: s.Dir, Args: []string{"for-each-ref", "--format=%(objectname) %(refname)", "refs/remotes", "refs/tags"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		return nil
	}
	return out
}
