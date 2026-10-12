// Package session manages one repository opened in a tab: graph loading, reads,
// serialized writes with the file watcher suspended, remote operations with
// progress and cancellation, and the events sent to the frontend.
package session

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v5"

	"gitrais/internal/diff"
	"gitrais/internal/gitexec"
	"gitrais/internal/gitread"
	"gitrais/internal/graph"
	"gitrais/internal/ops"
	"gitrais/internal/status"
	"gitrais/internal/watch"
)

// Emitter sends an event to the frontend. It is called from several goroutines
// (watcher, graph loader, callers of write and remote operations), possibly
// concurrently, so it must be safe for concurrent use and must not block for
// long nor call back into the Session. It is never called after Close returns.
type Emitter func(event string, data any)

// Event names.
const (
	EventGraphReset    = "graph:reset"    // TabEvent: the row index was replaced, refetch rows
	EventGraphProgress = "graph:progress" // GraphProgress
	EventWipChanged    = "wip:changed"    // TabEvent: working tree / index changed
	EventRefsChanged   = "refs:changed"   // TabEvent: branches, tags, HEAD, stashes changed
	EventOpProgress    = "op:progress"    // OpProgress
)

type TabEvent struct {
	Tab string `json:"tab"`
}

type GraphProgress struct {
	Tab   string `json:"tab"`
	Rows  int    `json:"rows"`
	Done  bool   `json:"done"`
	Error string `json:"error,omitempty"`
}

type OpProgress struct {
	Tab string `json:"tab"`
	Op  string `json:"op"`
	gitexec.Progress
}

var (
	// ErrClosed is returned by operations started after Close.
	ErrClosed = errors.New("repository tab is closed")
	// ErrOpInProgress is returned when a remote operation (fetch, pull, push,
	// remote branch or tag push/delete) is requested while another one runs.
	ErrOpInProgress = errors.New("another remote operation is in progress")
	// ErrInvalidHash is returned for a commit id that is not a hex object name.
	ErrInvalidHash = errors.New("invalid commit hash")
)

// Test seams. Tests set them before Open and restore them after Close.
var (
	readStashes = gitread.ReadStashes
	// beforeGraphProgress, when set, runs in the graph loader of generation gen
	// right before it tries to publish a progress or done event.
	beforeGraphProgress func(gen uint64)
)

// watchDebounce coalesces bursts of file events into one notification.
const watchDebounce = 300 * time.Millisecond

// Session is one open repository. All methods are safe for concurrent use.
//
// Concurrency:
//   - reads run concurrently and never take the write lock;
//   - writes are serialized by writeSem (one write at a time per repository) and
//     run with the watcher suspended, so the app's own changes do not echo back as
//     watcher events; the session emits wip:changed (and refs:changed plus a graph
//     reload when refs may have moved) itself once the write is over, also on error;
//   - at most one remote operation runs at a time (ErrOpInProgress otherwise); it
//     is cancellable with CancelOp, also while waiting for the write lock;
//   - go-git's *git.Repository is not safe for concurrent use, so every access to
//     it goes through repoMu.
type Session struct {
	ID, Dir, GitDir string

	r      *gitexec.Runner
	emitFn Emitter
	ctx    context.Context // lives until Close
	cancel context.CancelFunc

	emitMu sync.RWMutex // read-held while emitting; Close write-locks it to set closed
	closed bool
	once   sync.Once

	repoMu    sync.Mutex
	repo      *git.Repository
	repoStale atomic.Bool // refs changed: reopen before the next go-git read

	idx atomic.Pointer[graph.Index]

	loadMu     sync.Mutex // guards loadGen, loadCancel, loadClosed, loadWG.Add and writeWG.Add
	loadGen    uint64
	loadCancel context.CancelFunc
	loadClosed bool // set by Close: no new loader or write starts
	loadWG     sync.WaitGroup
	writeWG    sync.WaitGroup // in-flight writes (local and remote)

	writeSem chan struct{} // capacity 1: the per-repository write lock

	opMu     sync.Mutex
	opCancel context.CancelFunc // non-nil while a remote operation runs

	watchers []*watch.Watcher
}

// Open opens the repository containing dir and starts loading its graph. The
// returned Session must be closed with Close.
func Open(id, dir string, r *gitexec.Runner, emit Emitter) (*Session, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{ID: id, r: r, emitFn: emit, ctx: ctx, cancel: cancel, writeSem: make(chan struct{}, 1)}
	if err := s.open(dir); err != nil {
		s.Close()
		return nil, err
	}
	s.Reload()
	return s, nil
}

func (s *Session) open(dir string) error {
	top, err := revParse(s.ctx, s.r, dir, "--show-toplevel")
	if err != nil {
		return err
	}
	gitDir, err := revParse(s.ctx, s.r, top, "--absolute-git-dir")
	if err != nil {
		return err
	}
	commonDir, err := revParse(s.ctx, s.r, top, "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	s.Dir, s.GitDir = top, gitDir
	if s.repo, err = gitread.OpenRepo(top); err != nil {
		return err
	}

	// The worktree and its git dir; the git dir needs its own path when it does
	// not live under the worktree (linked worktree, --separate-git-dir).
	paths := []string{top}
	if !within(top, gitDir) {
		paths = append(paths, gitDir)
	}
	src, err := watch.NewSource(paths...)
	if err != nil {
		return err
	}
	// Without the ignore filter every change counts as WIP: only spurious refreshes.
	ignore, _ := watch.IgnoredPaths(s.ctx, s.r, top)
	s.watchers = append(s.watchers, watch.New(top, gitDir, src, watchDebounce, ignore, s.onFS))

	// A linked worktree's branches and tags live in the common git dir, which
	// neither path above covers: watch it as a git dir of its own. Its HEAD and
	// index belong to the main worktree and only cause spurious refreshes.
	if commonDir != gitDir && !within(top, commonDir) && !within(gitDir, commonDir) {
		csrc, err := watch.NewSource(commonDir)
		if err != nil {
			return err
		}
		s.watchers = append(s.watchers, watch.New(commonDir, commonDir, csrc, watchDebounce, nil, s.onFS))
	}
	return nil
}

// within reports whether p is root or inside it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func revParse(ctx context.Context, r *gitexec.Runner, dir string, flags ...string) (string, error) {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: append([]string{"rev-parse"}, flags...), Env: gitexec.ReadOnlyEnv})
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(strings.TrimSpace(string(out)))
}

// Close stops the watchers, cancels the graph load and every running write
// (local or remote), and waits for them. After it returns no event is emitted
// and no write runs. It is idempotent and must not be called from the Emitter.
func (s *Session) Close() {
	s.once.Do(func() {
		s.emitMu.Lock()
		s.closed = true
		s.emitMu.Unlock()

		s.cancel() // also cancels writes, remote ops and the ignore filter

		s.loadMu.Lock()
		s.loadClosed = true
		if s.loadCancel != nil {
			s.loadCancel()
		}
		s.loadMu.Unlock()

		// Watchers first: a cancelled write's resume then skips the flush
		// barrier. Writes take no lock Close holds, so waiting cannot deadlock.
		for _, w := range s.watchers {
			w.Close()
		}
		s.writeWG.Wait()
		s.loadWG.Wait()
	})
}

func (s *Session) emit(event string, data any) {
	s.emitMu.RLock()
	defer s.emitMu.RUnlock()
	if !s.closed {
		s.emitFn(event, data)
	}
}

// onFS is the watchers' callback. It must not block (Close waits for it): it
// only emits, marks the go-git handle stale and starts an asynchronous reload.
func (s *Session) onFS(k watch.Kind) {
	if k&watch.WIP != 0 {
		s.emit(EventWipChanged, TabEvent{s.ID})
	}
	if k&watch.Refs != 0 {
		s.refsChanged()
	}
}

// refsChanged notifies the frontend and rebuilds the graph. Non-blocking.
func (s *Session) refsChanged() {
	// go-git caches packfiles and refs: reopen on the next read so that new
	// objects (fetched, committed elsewhere) are visible.
	s.repoStale.Store(true)
	s.emit(EventRefsChanged, TabEvent{s.ID})
	s.Reload()
}

// Reload rebuilds the graph in the background. The view keeps the previous index
// until the new one has its first batch (5000 rows) or is complete, then
// graph:reset is emitted. A newer Reload cancels an older one.
func (s *Session) Reload() {
	s.loadMu.Lock()
	if s.loadClosed {
		s.loadMu.Unlock()
		return
	}
	if s.loadCancel != nil {
		s.loadCancel()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.loadCancel = cancel
	s.loadGen++
	gen := s.loadGen
	s.loadWG.Add(1)
	s.loadMu.Unlock()

	go func() {
		defer s.loadWG.Done()
		defer cancel()
		idx := graph.NewIndex()
		swapped := false
		// publish installs idx on its first call and emits gp (if any), unless a
		// newer Reload or Close superseded this loader. The check, the store and
		// the emits all happen under loadMu, so once a newer generation exists
		// (it is created under loadMu, before its graph:reset) nothing from this
		// one is emitted. Lock order: loadMu, then emitMu; Close never takes them
		// nested and the Emitter must not call back into the Session.
		publish := func(gp *GraphProgress) bool {
			if gp != nil && beforeGraphProgress != nil {
				beforeGraphProgress(gen)
			}
			s.loadMu.Lock()
			defer s.loadMu.Unlock()
			if gen != s.loadGen || s.loadClosed || ctx.Err() != nil {
				return false
			}
			if !swapped {
				swapped = true
				s.idx.Store(idx)
				s.emit(EventGraphReset, TabEvent{s.ID})
			}
			if gp != nil {
				s.emit(EventGraphProgress, *gp)
			}
			return true
		}
		if s.idx.Load() == nil {
			publish(nil) // first load: show rows as they arrive
		}
		// Without stashes the graph is still useful: a failed stash read only
		// leaves the stash rows out.
		stashes, _ := readStashes(ctx, s.r, s.Dir)
		err := graph.Load(ctx, s.r, s.Dir, idx, graph.LoadOptions{
			Stashes: stashes,
			OnProgress: func(n int) {
				publish(&GraphProgress{Tab: s.ID, Rows: n})
			},
		})
		if ctx.Err() != nil {
			return
		}
		gp := GraphProgress{Tab: s.ID, Rows: idx.Len(), Done: true}
		if err != nil {
			gp.Error = err.Error()
		}
		publish(&gp)
	}()
}

// Rows returns up to count graph rows starting at start (never nil).
func (s *Session) Rows(start, count int) []graph.Row {
	if idx := s.idx.Load(); idx != nil {
		if rows := idx.Rows(start, count); rows != nil {
			return rows
		}
	}
	return []graph.Row{}
}

// GraphInfo returns the number of loaded rows and whether loading finished.
func (s *Session) GraphInfo() (rows int, done bool) {
	if idx := s.idx.Load(); idx != nil {
		return idx.Len(), idx.Done()
	}
	return 0, false
}

// Find returns the row of hash, or -1.
func (s *Session) Find(hash string) int {
	if idx := s.idx.Load(); idx != nil {
		if i, ok := idx.Find(hash); ok {
			return i
		}
	}
	return -1
}

func (s *Session) Refs(ctx context.Context) (gitread.Refs, error) {
	return gitread.ReadRefs(ctx, s.r, s.Dir)
}

func (s *Session) Stashes(ctx context.Context) ([]graph.Stash, error) {
	return gitread.ReadStashes(ctx, s.r, s.Dir)
}

func (s *Session) Status(ctx context.Context) (status.Status, error) {
	return status.Read(ctx, s.r, s.Dir)
}

var hashRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func validHash(h string) error {
	if !hashRe.MatchString(h) {
		return ErrInvalidHash
	}
	return nil
}

// Commit returns the metadata and changed files of a commit (full hash).
// Calls are serialized on the go-git handle (not safe for concurrent use).
func (s *Session) Commit(ctx context.Context, hash string) (gitread.CommitDetail, error) {
	if err := validHash(hash); err != nil {
		return gitread.CommitDetail{}, err
	}
	s.repoMu.Lock()
	defer s.repoMu.Unlock()
	if s.repoStale.Swap(false) {
		repo, err := gitread.OpenRepo(s.Dir)
		if err != nil {
			s.repoStale.Store(true)
			return gitread.CommitDetail{}, err
		}
		s.repo = repo
	}
	return gitread.ReadCommit(ctx, s.r, s.repo, s.Dir, hash)
}

func (s *Session) WipDiff(ctx context.Context, req ops.WipDiffRequest) (diff.FileDiff, error) {
	return ops.WipDiff(ctx, s.r, s.Dir, req)
}

// FileVersions returns the old and new contents of a file for the diff
// viewer (see ops.FileVersions). Commit, when set, must be a full hash.
func (s *Session) FileVersions(ctx context.Context, req ops.VersionsRequest) (ops.Versions, error) {
	if req.Commit != "" {
		if err := validHash(req.Commit); err != nil {
			return ops.Versions{}, err
		}
	}
	return ops.FileVersions(ctx, s.r, s.Dir, req)
}

// CommitFileDiff returns the diff of one file in a commit against its first
// parent (the empty tree for a root commit). oldPath is the pre-rename path.
func (s *Session) CommitFileDiff(ctx context.Context, hash, path, oldPath string) (diff.FileDiff, error) {
	if err := validHash(hash); err != nil {
		return diff.FileDiff{}, err
	}
	from := ""
	out, err := s.r.Run(ctx, gitexec.Cmd{Dir: s.Dir, Args: []string{"rev-parse", "-q", "--verify", hash + "^1"}, Env: gitexec.ReadOnlyEnv})
	switch {
	case err == nil:
		from = strings.TrimSpace(string(out))
	case gitexec.ExitCode(err) == 1:
		if from, err = gitread.EmptyTree(ctx, s.r, s.Dir); err != nil {
			return diff.FileDiff{}, err
		}
	default:
		return diff.FileDiff{}, err
	}
	return ops.CommitFileDiff(ctx, s.r, s.Dir, from, hash, path, oldPath)
}

func (s *Session) LastCommitMessage(ctx context.Context) (string, error) {
	return ops.LastCommitMessage(ctx, s.r, s.Dir)
}

// lock acquires the write lock, giving up when ctx or the session is done.
func (s *Session) lock(ctx context.Context) error {
	if s.ctx.Err() != nil {
		return ErrClosed
	}
	select {
	case s.writeSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		if s.ctx.Err() != nil {
			return ErrClosed
		}
		return ctx.Err()
	case <-s.ctx.Done():
		return ErrClosed
	}
}

func (s *Session) unlock() { <-s.writeSem }

// write serializes a write, suspends the watchers (dropping the events the write
// itself causes) and notifies the frontend when it is over, even on error.
// refs says whether refs/HEAD may have moved (graph reload) or only the
// working tree and index changed. Errors from fn are returned unchanged. fn runs
// under a context cancelled by ctx or by Close, which waits for it.
func (s *Session) write(ctx context.Context, refs bool, fn func(context.Context) error) error {
	s.loadMu.Lock()
	if s.loadClosed {
		s.loadMu.Unlock()
		return ErrClosed
	}
	s.writeWG.Add(1)
	s.loadMu.Unlock()
	defer s.writeWG.Done()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()

	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	resumes := make([]func(bool), 0, len(s.watchers))
	for _, w := range s.watchers {
		resumes = append(resumes, w.Suspend())
	}
	err := fn(ctx)
	// resume(false) waits briefly for FSEvents to deliver the write's own events
	// so they are discarded. It must not run under a lock onFS needs: onFS takes
	// none that is held here.
	for _, resume := range resumes {
		resume(false)
	}
	s.emit(EventWipChanged, TabEvent{s.ID})
	if refs {
		s.refsChanged()
	}
	return err
}

// opRunning reports whether a remote operation is in progress.
func (s *Session) opRunning() bool {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.opCancel != nil
}

// remote runs a network operation: only one at a time (ErrOpInProgress),
// cancellable with CancelOp, by ctx or by Close, emitting op:progress. It is a
// write (it updates refs, and pull the working tree). A cancelled operation
// returns an error matching context.Canceled.
func (s *Session) remote(ctx context.Context, op string, fn func(context.Context, ops.OnProgress) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	octx, cancel := context.WithCancel(s.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	s.opMu.Lock()
	if s.opCancel != nil {
		s.opMu.Unlock()
		cancel()
		return ErrOpInProgress
	}
	s.opCancel = cancel
	s.opMu.Unlock()
	defer func() {
		s.opMu.Lock()
		s.opCancel = nil
		s.opMu.Unlock()
		cancel()
	}()
	// gitexec returns ctx.Err() for a cancelled command.
	return s.write(octx, true, func(c context.Context) error {
		return fn(c, func(p gitexec.Progress) { s.emit(EventOpProgress, OpProgress{Tab: s.ID, Op: op, Progress: p}) })
	})
}

// CancelOp cancels the running remote operation, if any.
func (s *Session) CancelOp() {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.opCancel != nil {
		s.opCancel()
	}
}
