package watch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gitrais/internal/gitexec"
)

// excludeRel is what the Watcher passes to its ignore predicate when
// <gitDir>/info/exclude changes. A worktree path never looks like this (Classify
// drops root/.git/... before asking), so it is unambiguous.
const excludeRel = ".git/info/exclude"

const (
	defaultRebuildDelay = 100 * time.Millisecond
	defaultMinInterval  = 2 * time.Second
)

// IgnoreFilter answers whether a worktree-relative slash path is ignored by
// gitignore rules (.gitignore files, info/exclude, core.excludesFile).
//
// Cost model: one `git ls-files --others --ignored --exclude-standard --directory`
// per snapshot (a single worktree walk; wholly ignored directories such as
// node_modules are reported as one entry and not descended into, so the cost is
// roughly that of `git status` on untracked files). Ignored is an in-memory map
// lookup and never runs git.
//
// The snapshot is rebuilt in the background, never on the caller's goroutine;
// until the new one is ready (and forever if the rebuild fails) the old one stays
// in use. A rebuild is scheduled when:
//   - Ignored is asked about a .gitignore file or about ".git/info/exclude" (the
//     rules changed): the snapshot is marked stale;
//   - Ignored is asked about a path whose first component did not exist (or was
//     an empty directory) when the snapshot was taken, e.g. a node_modules or
//     build directory created later.
//
// Requests are debounced (one rebuild for a burst of changes) and rebuilds start
// at least 2s apart. Until a rebuild completes, a new ignored path is reported as
// not ignored, which only costs a spurious WIP refresh. Deeper new directories
// (a fresh node_modules inside an existing package dir) are only picked up by the
// next rebuild that something else triggers.
type IgnoreFilter struct {
	r   *gitexec.Runner
	dir string

	ctx    context.Context // owned by the filter, cancelled by Close
	cancel context.CancelFunc
	snap   atomic.Pointer[ignoredSnapshot]

	mu          sync.Mutex
	timer       *time.Timer
	running     bool
	again       bool // a request arrived while a rebuild was running
	lastStart   time.Time
	closed      bool
	wg          sync.WaitGroup
	delay       time.Duration // debounce before a rebuild
	minInterval time.Duration // minimum spacing between rebuild starts

	rebuilds  atomic.Int32 // completed rebuild attempts (tests)
	onRebuild func(error)  // test hook, called after each attempt
}

// NewIgnoreFilter takes the first snapshot with ctx. Later rebuilds use a context
// owned by the filter, independent of ctx and cancelled by Close.
func NewIgnoreFilter(ctx context.Context, r *gitexec.Runner, dir string) (*IgnoreFilter, error) {
	snap, err := loadIgnored(ctx, r, dir)
	if err != nil {
		return nil, err
	}
	bg, cancel := context.WithCancel(context.Background())
	f := &IgnoreFilter{r: r, dir: dir, ctx: bg, cancel: cancel,
		delay: defaultRebuildDelay, minInterval: defaultMinInterval}
	f.snap.Store(snap)
	return f, nil
}

// IgnoredPaths returns IgnoreFilter.Ignored for dir. ctx is used for the first
// snapshot and bounds the filter's lifetime: when ctx is done the filter is closed
// (no more rebuilds; the last snapshot keeps answering). Rebuilds themselves run
// with the filter's own context, not with ctx, so pass a context that lives as
// long as the watcher, or use NewIgnoreFilter and Close.
func IgnoredPaths(ctx context.Context, r *gitexec.Runner, dir string) (func(rel string) bool, error) {
	f, err := NewIgnoreFilter(ctx, r, dir)
	if err != nil {
		return nil, err
	}
	context.AfterFunc(ctx, func() { f.Close() })
	return f.Ignored, nil
}

// Ignored reports whether rel (worktree-relative, slash separated), or one of its
// ancestor directories, is ignored. It never blocks on git. Safe for concurrent use.
func (f *IgnoreFilter) Ignored(rel string) bool {
	rel = strings.TrimSuffix(rel, "/")
	if rel == ".gitignore" || strings.HasSuffix(rel, "/.gitignore") || rel == excludeRel {
		f.request()
		return false
	}
	s := f.snap.Load()
	if s.match(rel) {
		return true
	}
	first, _, _ := strings.Cut(rel, "/")
	if first != "" && first != "." && !s.top[first] {
		f.request() // created after the snapshot: it may be an ignored directory
	}
	return false
}

// Close stops future rebuilds, cancels one in progress and waits for it.
// Ignored keeps answering from the last snapshot. Idempotent.
func (f *IgnoreFilter) Close() error {
	f.mu.Lock()
	f.closed = true
	if f.timer != nil && f.timer.Stop() {
		f.wg.Done() // the timer's run will never start
	}
	f.timer = nil
	f.mu.Unlock()
	f.cancel()
	f.wg.Wait()
	return nil
}

// request schedules a rebuild unless one is already pending; while one runs, it
// asks for another after it.
func (f *IgnoreFilter) request() {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.closed || f.timer != nil:
	case f.running:
		f.again = true
	default:
		f.scheduleLocked()
	}
}

// scheduleLocked arms the rebuild timer; f.mu must be held.
func (f *IgnoreFilter) scheduleLocked() {
	d := f.delay
	if !f.lastStart.IsZero() {
		if wait := time.Until(f.lastStart.Add(f.minInterval)); wait > d {
			d = wait
		}
	}
	f.wg.Add(1)
	f.timer = time.AfterFunc(d, f.run)
}

func (f *IgnoreFilter) run() {
	defer f.wg.Done()
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.timer = nil
	f.running = true
	f.lastStart = time.Now()
	f.mu.Unlock()

	next, err := loadIgnored(f.ctx, f.r, f.dir)
	if err == nil {
		f.snap.Store(next)
	}

	f.mu.Lock()
	f.running = false
	if f.again && !f.closed {
		f.again = false
		f.scheduleLocked()
	}
	f.mu.Unlock()
	f.rebuilds.Add(1)
	if f.onRebuild != nil {
		f.onRebuild(err)
	}
}

type ignoredSnapshot struct {
	files map[string]bool // ignored files
	dirs  map[string]bool // wholly ignored directories, without trailing slash
	top   map[string]bool // top-level entries known when the snapshot was taken
}

func loadIgnored(ctx context.Context, r *gitexec.Runner, dir string) (*ignoredSnapshot, error) {
	// List the top level before the walk: whatever is known here was seen by it.
	top, err := topLevel(dir)
	if err != nil {
		return nil, err
	}
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Env: gitexec.ReadOnlyEnv,
		Args: []string{"ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z"}})
	if err != nil {
		return nil, err
	}
	s := &ignoredSnapshot{files: map[string]bool{}, dirs: map[string]bool{}, top: top}
	for _, p := range strings.Split(string(out), "\x00") {
		switch {
		case p == "":
		case strings.HasSuffix(p, "/"):
			s.dirs[strings.TrimSuffix(p, "/")] = true
		default:
			s.files[p] = true
		}
	}
	return s, nil
}

// topLevel returns the names in dir, leaving out empty directories: git does not
// report those, so a later file inside one must still trigger a rebuild.
func topLevel(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	top := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() && isEmptyDir(filepath.Join(dir, e.Name())) {
			continue
		}
		top[e.Name()] = true
	}
	return top, nil
}

func isEmptyDir(p string) bool {
	d, err := os.Open(p)
	if err != nil {
		return false
	}
	defer d.Close()
	names, _ := d.Readdirnames(1)
	return len(names) == 0
}

// match reports whether rel, or any ancestor directory of it, is ignored.
func (s *ignoredSnapshot) match(rel string) bool {
	if s.files[rel] || s.dirs[rel] {
		return true
	}
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' && s.dirs[rel[:i]] {
			return true
		}
	}
	return false
}
