// Package app is the single point of contact between Wails and the backend.
// Its exported methods are bound to the frontend; they stay thin and delegate
// to session, store and ops. Every returned error's text is a UIError JSON.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"gitrais/internal/askpass"
	"gitrais/internal/gitexec"
	"gitrais/internal/session"
	"gitrais/internal/store"
)

// EnvLogPath, when set to a non-empty value, overrides the activity log
// location (used by E2E tests, together with store.EnvStatePath).
const EnvLogPath = "GITRAIS_LOG_PATH"

// shutdownTimeout bounds how long Shutdown waits for sessions to close (a
// Session.Close can take up to ~7 s when it has to kill a git process).
const shutdownTimeout = 10 * time.Second

type Config struct {
	StatePath, LogPath, Git, Helper string
}

// tabEntry is an open tab. mu/dropped gate the session's events: once
// CloseTab or Shutdown sets dropped, no further event of the tab is emitted.
type tabEntry struct {
	s       *session.Session
	mu      sync.RWMutex
	dropped bool
}

func (e *tabEntry) drop() {
	e.mu.Lock()
	e.dropped = true
	e.mu.Unlock()
}

type App struct {
	ctx    context.Context // app lifetime: cancelled by Shutdown
	cancel context.CancelFunc

	cfg        Config
	startupErr string
	runner     *gitexec.Runner
	ap         *askpass.Server
	store      *store.Store
	log        *gitexec.ActivityLog

	mu       sync.Mutex
	sessions map[string]*tabEntry
	closed   bool
	closeWG  sync.WaitGroup // sessions (and the askpass server) being closed

	authMu     sync.Mutex
	authQueue  []*authReq // head = prompt currently shown
	authClosed bool
	done       chan struct{} // closed by Shutdown

	cloneMu     sync.Mutex
	cloneCancel context.CancelFunc

	// Test seams (nil/zero in production).
	authTimeoutOverride time.Duration   // replaces authTimeout when > 0
	beforeRestoreOpen   func(id string) // called before RestoreTabs opens a saved tab
	beforeAskpassClose  func()          // called by shutdown right before the askpass server is closed

	emitFn  func(string, any)
	pickDir func(title string) (string, error)
}

func New() *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{ctx: ctx, cancel: cancel, sessions: map[string]*tabEntry{}, done: make(chan struct{})}
}

// NewWithConfig creates an App without the Wails runtime (tests, E2E). The
// returned App is always usable; err reports startup problems (also exposed by
// StartupError), e.g. a corrupt state file (wrapping store.ErrCorrupt) or a
// missing git.
func NewWithConfig(cfg Config) (*App, error) {
	a := New()
	return a, a.setup(cfg)
}

// defaultConfig resolves the production configuration. Problems are returned
// alongside a partial Config so the app can still start and show them.
func defaultConfig() (Config, []error) {
	var cfg Config
	var problems []error
	var err error
	if cfg.StatePath, err = store.DefaultPath(); err != nil {
		problems = append(problems, fmt.Errorf("state file location: %w", err))
	}
	if cfg.LogPath = os.Getenv(EnvLogPath); cfg.LogPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.LogPath = filepath.Join(home, "Library", "Logs", "Gitrais", "activity.log")
		}
	}
	if cfg.Git, err = gitexec.Locate(); err != nil {
		problems = append(problems, fmt.Errorf("%w (%v)", errGitMissing, err))
	}
	if cfg.Helper, err = gitexec.LocateHelper(); err != nil {
		problems = append(problems, fmt.Errorf("the askpass helper was not found, credential prompts are unavailable (%v)", err))
	}
	return cfg, problems
}

// setup initialises the backend. It never leaves the App unusable: every
// problem is collected, stored for StartupError and returned joined.
func (a *App) setup(cfg Config, problems ...error) error {
	if cfg.StatePath == "" {
		cfg.StatePath = filepath.Join(os.TempDir(), "gitrais-state.json")
		problems = append(problems, fmt.Errorf("UI state is kept in %s", cfg.StatePath))
	}
	a.cfg = cfg
	a.store = store.Open(cfg.StatePath)
	// Load at startup is where corruption is reported (the file is quarantined).
	if _, err := a.store.Load(); err != nil {
		problems = append(problems, fmt.Errorf("UI state was reset: %w", err))
	}
	if cfg.LogPath != "" {
		log, err := gitexec.OpenActivityLog(cfg.LogPath)
		if err != nil {
			problems = append(problems, fmt.Errorf("activity log unavailable: %w", err))
		}
		a.log = log
	}
	a.runner = &gitexec.Runner{Git: cfg.Git, Log: a.log}
	if cfg.Git == "" && !containsErr(problems, errGitMissing) {
		problems = append(problems, errGitMissing)
	}
	if cfg.Helper != "" {
		ap, err := askpass.Start(a.askHandler)
		if err != nil {
			problems = append(problems, fmt.Errorf("credential prompts unavailable: %w", err))
		} else {
			a.ap = ap
			a.runner.Env = append(a.runner.Env, ap.Env(cfg.Helper)...)
		}
	}
	err := errors.Join(problems...)
	if err != nil {
		a.startupErr = err.Error()
	}
	return err
}

func containsErr(errs []error, target error) bool {
	for _, e := range errs {
		if errors.Is(e, target) {
			return true
		}
	}
	return false
}

// Startup is the Wails OnStartup hook. It is a package function (not a method)
// so Wails does not bind it to the frontend.
func Startup(a *App, ctx context.Context) {
	a.emitFn = func(ev string, d any) { runtime.EventsEmit(ctx, ev, d) }
	a.pickDir = func(title string) (string, error) {
		return runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
	}
	cfg, problems := defaultConfig()
	a.setup(cfg, problems...)
}

// Shutdown is the Wails OnShutdown hook (a package function, see Startup).
func Shutdown(a *App, ctx context.Context) { a.shutdown() }

// shutdown closes every session concurrently and waits for them (bounded by
// shutdownTimeout) so git processes are not orphaned. It is idempotent.
func (a *App) shutdown() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	for id, e := range a.sessions {
		a.closeLocked(e)
		delete(a.sessions, id)
	}
	a.mu.Unlock()

	a.cancel() // aborts reads and a running clone
	a.closeAuth()
	close(a.done) // releases pending auth prompts
	a.cloneMu.Lock()
	if a.cloneCancel != nil {
		a.cloneCancel()
	}
	a.cloneMu.Unlock()

	// Order matters: the askpass server is closed only after every session is
	// closed, so a git process blocked on a credential prompt is killed by its
	// session instead of failing mid-way because its helper lost the server.
	// Pending prompts were already released above (closeAuth, a.done). Both
	// phases share the shutdownTimeout budget.
	deadline := time.After(shutdownTimeout)
	if !a.waitClosed(deadline) {
		return // leave the server and log open: a late session may still use them
	}
	if a.ap != nil {
		if a.beforeAskpassClose != nil {
			a.beforeAskpassClose()
		}
		a.closeWG.Add(1)
		go func() {
			defer a.closeWG.Done()
			a.ap.Close()
		}()
		if !a.waitClosed(deadline) {
			return
		}
	}
	a.log.Close()
}

// waitClosed waits for the background closers or the deadline.
func (a *App) waitClosed(deadline <-chan time.Time) bool {
	finished := make(chan struct{})
	go func() {
		a.closeWG.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return true
	case <-deadline:
		return false
	}
}

// closeLocked drops the tab's events and closes its session in the background
// (Close can take seconds). a.mu must be held.
func (a *App) closeLocked(e *tabEntry) {
	e.drop()
	a.closeWG.Add(1)
	go func() {
		defer a.closeWG.Done()
		e.s.Close()
	}()
}

func (a *App) emit(ev string, d any) {
	if a.emitFn != nil {
		a.emitFn(ev, d)
	}
}

// ready reports whether repository operations can run.
func (a *App) ready() error {
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return session.ErrClosed
	}
	if a.cfg.Git == "" {
		return errGitMissing
	}
	return nil
}

func (a *App) Version() string { return "0.1.0" }

// StartupError describes the problems met at startup (missing git or askpass
// helper, corrupt state file reset to defaults, ...), one per line; "" if none.
// The app keeps running: the frontend shows it as a banner.
func (a *App) StartupError() string { return a.startupErr }

// interrupted reports whether err (may be nil) or the app state means the
// app is closing, as opposed to a repository that cannot be opened.
func (a *App) interrupted(err error) bool {
	if errors.Is(err, session.ErrClosed) || errors.Is(err, context.Canceled) {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed
}

func (a *App) sess(tab string) (*session.Session, error) {
	if err := a.ready(); err != nil {
		return nil, err // after Shutdown: Cancelled rather than NoTab
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.sessions[tab]
	if !ok {
		return nil, errNoTab
	}
	return e.s, nil
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// OpenActivityLog opens the git activity log in the default viewer.
func (a *App) OpenActivityLog() error {
	if a.cfg.LogPath == "" {
		return uiErr(errors.New("no activity log"))
	}
	return uiErr(exec.Command("open", a.cfg.LogPath).Run())
}
