package app

import (
	"context"
	"slices"
	"strings"
	"time"
)

// Event names emitted by the app layer (the session ones are in package session).
const (
	EventAuthPrompt = "auth:prompt" // AuthPrompt: show a credential dialog
	EventAuthCancel = "auth:cancel" // AuthCancel: close the dialog of that prompt
)

// authTimeout bounds how long a prompt waits for the user.
const authTimeout = 5 * time.Minute

type authAnswer struct {
	value string
	ok    bool
}

type authReq struct {
	p  AuthPrompt
	ch chan authAnswer // buffered (1): AnswerAuth never blocks
	// shown is closed when the prompt becomes the head of the queue; the
	// timeout runs from then on, not while it waits behind other prompts.
	shown chan struct{}
}

// showLocked emits the prompt and starts its timeout. authMu must be held.
func (a *App) showLocked(req *authReq) {
	a.emit(EventAuthPrompt, req.p)
	close(req.shown)
}

// isSecret reports whether the prompt asks for a secret (the input must be
// masked). Username and host-key confirmation prompts are not secret.
func isSecret(prompt string) bool {
	p := strings.ToLower(prompt)
	return strings.Contains(p, "password") || strings.Contains(p, "passphrase") || strings.Contains(p, "token")
}

// askHandler is the askpass.Handler. Prompts are serialized for the UI: only
// the head of the queue is shown (auth:prompt); the others wait their turn.
// When a shown prompt ends without an answer (ctx cancelled because the helper
// died or the operation was cancelled, timeout, shutdown) auth:cancel is
// emitted so the frontend closes the dialog. Prompts and answers are never
// logged.
func (a *App) askHandler(ctx context.Context, prompt string) (string, bool) {
	req := &authReq{p: AuthPrompt{ID: newID(), Prompt: prompt, Secret: isSecret(prompt)}, ch: make(chan authAnswer, 1), shown: make(chan struct{})}
	a.authMu.Lock()
	if a.authClosed {
		a.authMu.Unlock()
		return "", false
	}
	a.authQueue = append(a.authQueue, req)
	if len(a.authQueue) == 1 {
		a.showLocked(req)
	}
	a.authMu.Unlock()

	var timeout <-chan time.Time // nil until the prompt is shown
	shown := req.shown
	var ans authAnswer
	answered := false
wait:
	for {
		select {
		case ans = <-req.ch:
			answered = true
			break wait
		case <-ctx.Done():
			break wait
		case <-shown:
			shown = nil
			timer := time.NewTimer(a.authTimeoutFor())
			defer timer.Stop()
			timeout = timer.C
		case <-timeout:
			break wait
		case <-a.done:
			break wait
		}
	}

	a.authMu.Lock()
	defer a.authMu.Unlock()
	if !answered { // an answer may have raced with the cancellation
		select {
		case ans = <-req.ch:
			answered = true
		default:
		}
	}
	i := slices.Index(a.authQueue, req)
	if i < 0 {
		return ans.value, answered && ans.ok
	}
	a.authQueue = slices.Delete(a.authQueue, i, i+1)
	if i == 0 {
		if !answered {
			a.emit(EventAuthCancel, AuthCancel{ID: req.p.ID})
		}
		if len(a.authQueue) > 0 && !a.authClosed {
			a.showLocked(a.authQueue[0])
		}
	}
	if answered && ctx.Err() == nil {
		return ans.value, ans.ok
	}
	return "", false
}

// AnswerAuth answers the prompt currently shown. ok=false means the user
// cancelled. Answers for unknown or no longer pending prompts are ignored.
func (a *App) AnswerAuth(id, value string, ok bool) {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if len(a.authQueue) == 0 || a.authQueue[0].p.ID != id {
		return
	}
	select {
	case a.authQueue[0].ch <- authAnswer{value: value, ok: ok}:
	default: // already answered
	}
}

// closeAuth releases every pending prompt (shutdown).
func (a *App) closeAuth() {
	a.authMu.Lock()
	a.authClosed = true
	a.authMu.Unlock()
}

func (a *App) authTimeoutFor() time.Duration {
	if a.authTimeoutOverride > 0 {
		return a.authTimeoutOverride
	}
	return authTimeout
}
