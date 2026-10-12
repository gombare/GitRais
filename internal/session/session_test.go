package session

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gitrais/internal/gitexec"
	"gitrais/internal/ops"
	"gitrais/internal/testutil"
)

type recorder struct {
	mu     sync.Mutex
	events []string
	data   []any
	ch     chan struct{}
}

func newRecorder() *recorder { return &recorder{ch: make(chan struct{}, 1000)} }

func (r *recorder) emit(ev string, d any) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.data = append(r.data, d)
	r.mu.Unlock()
	select {
	case r.ch <- struct{}{}:
	default:
	}
}

func (r *recorder) count(ev string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e == ev {
			n++
		}
	}
	return n
}

func (r *recorder) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// waitFor waits up to 5s for an event matching pred.
func (r *recorder) waitFor(t *testing.T, ev string, pred func(any) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	seen := 0
	for {
		r.mu.Lock()
		for ; seen < len(r.events); seen++ {
			if r.events[seen] == ev && (pred == nil || pred(r.data[seen])) {
				r.mu.Unlock()
				return
			}
		}
		r.mu.Unlock()
		select {
		case <-r.ch:
		case <-time.After(50 * time.Millisecond): // the channel may have dropped a wake-up
		case <-deadline:
			t.Fatalf("event %s not received", ev)
		}
	}
}

func graphDone(rows int) func(any) bool {
	return func(d any) bool { g := d.(GraphProgress); return g.Done && g.Rows == rows }
}

func open(t *testing.T, repo *testutil.Repo) (*Session, *recorder) {
	t.Helper()
	rec := newRecorder()
	s, err := Open("t1", repo.Dir, repo.R, rec.emit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, rec
}

var bg = context.Background()

func TestOpenLoadsGraph(t *testing.T) {
	repo := testutil.NewRepo(t)
	for _, m := range []string{"a", "b", "c"} {
		repo.CommitAll(m)
	}
	s, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(3))
	if rows := s.Rows(0, 10); len(rows) != 3 || rows[0].Subject != "c" {
		t.Fatalf("rows = %+v", rows)
	}
	if s.Find(rows0(s)) != 0 || s.Find("deadbeef") != -1 {
		t.Fatal("Find")
	}
	if n, done := s.GraphInfo(); n != 3 || !done {
		t.Fatalf("GraphInfo = %d, %v", n, done)
	}
}

func rows0(s *Session) string { return s.Rows(0, 1)[0].Hash }

func TestWriteOpsEmitAndReload(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("one")
	s, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(1))
	repo.Write("a.txt", "2\n")
	before := rec.count("graph:progress")
	if err := s.StageAll(bg); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, "wip:changed", nil)
	time.Sleep(500 * time.Millisecond) // staging must not reload the graph
	if rec.count("graph:progress") != before {
		t.Fatal("staging must not reload the graph")
	}
	if _, err := s.CommitChanges(bg, ops.CommitOptions{Message: "two"}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, "refs:changed", nil)
	rec.waitFor(t, "graph:progress", graphDone(2))
	if s.Rows(0, 1)[0].Subject != "two" {
		t.Fatal("graph not updated after the commit")
	}
}

func TestStageLines(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\nb\nc\n")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\nB\nc\nd\n")
	s, _ := open(t, repo)
	fd, err := s.WipDiff(bg, ops.WipDiffRequest{Path: "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	addD := -1
	for i, l := range fd.Hunks[0].Lines {
		if l.Kind == "+" && l.Text == "d" {
			addD = i
		}
	}
	if err := s.StageLines(bg, StageLinesRequest{Path: "f.txt", Hunk: 0, Lines: []int{addD}}); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("diff", "--cached", "--numstat"); got != "1\t0\tf.txt" {
		t.Fatalf("staged = %q", got)
	}
	if err := s.StageLines(bg, StageLinesRequest{Path: "f.txt", Staged: true, Hunk: 0}); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("diff", "--cached", "--numstat"); got != "" {
		t.Fatalf("after unstage = %q", got)
	}
	if err := s.StageLines(bg, StageLinesRequest{Path: "f.txt", Hunk: 7}); !errors.Is(err, ErrHunkOutOfRange) {
		t.Fatalf("out-of-range hunk: err = %v", err)
	}
	repo.Write("new.txt", "x\n")
	repo.Git("add", "new.txt")
	if err := s.StageLines(bg, StageLinesRequest{Path: "new.txt", Staged: true, Hunk: 0}); !errors.Is(err, ErrPartialStageUnsupported) {
		t.Fatalf("added file: err = %v", err)
	}
}

func TestPushCurrentSetsUpstream(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "origin", bare)
	s, _ := open(t, repo)
	if err := s.PushCurrent(false); err != nil {
		t.Fatal(err)
	}
	if repo.Git("rev-parse", "--abbrev-ref", "main@{upstream}") != "origin/main" {
		t.Fatal("upstream not set")
	}
	repo.CommitAll("two")
	if err := s.PushCurrent(false); err != nil {
		t.Fatal(err)
	}
	if err := s.Fetch(); err != nil {
		t.Fatal(err)
	}
	if err := s.Pull(ops.PullFFOnly); err != nil {
		t.Fatal(err)
	}
}

// A remote whose name contains a slash must still be split correctly.
func TestPushCurrentRemoteWithSlash(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "team/x", bare)
	repo.Git("push", "-q", "-u", "team/x", "main:feat")
	repo.CommitAll("two")
	s, _ := open(t, repo)
	if err := s.PushCurrent(false); err != nil {
		t.Fatal(err)
	}
	if got, want := repo.Git("rev-parse", "refs/remotes/team/x/feat"), repo.Git("rev-parse", "HEAD"); got != want {
		t.Fatalf("pushed %s, want %s", got, want)
	}
}

func TestPushCurrentErrors(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	s, _ := open(t, repo)
	if err := s.PushCurrent(false); !errors.Is(err, ErrNoRemote) {
		t.Fatalf("no remote: err = %v", err)
	}
	repo.Git("checkout", "-q", "--detach")
	if err := s.PushCurrent(false); !errors.Is(err, ErrNoBranch) {
		t.Fatalf("detached: err = %v", err)
	}
}

func TestExternalChangeDetected(t *testing.T) {
	if testing.Short() {
		t.Skip("fsevents")
	}
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	_, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(1))
	time.Sleep(300 * time.Millisecond)
	repo.Write("ext.txt", "x\n")
	rec.waitFor(t, "wip:changed", nil)
	repo.CommitAll("external commit")
	rec.waitFor(t, "graph:progress", graphDone(2))
}

func TestReadsCommitAndDiffs(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\n")
	root := repo.CommitAll("root")
	repo.Write("f.txt", "a\nb\n")
	second := repo.CommitAll("second\n\nbody")
	s, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(2))

	// Concurrent detail reads exercise the go-git serialization under -race.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := s.Commit(bg, second)
			if err != nil || d.Summary != "second" || len(d.Files) != 1 {
				t.Errorf("Commit = %+v, %v", d, err)
			}
		}()
	}
	wg.Wait()

	fd, err := s.CommitFileDiff(bg, root, "f.txt", "")
	if err != nil || fd.Status != "A" {
		t.Fatalf("root diff = %+v, %v", fd, err)
	}
	fd, err = s.CommitFileDiff(bg, second, "f.txt", "")
	if err != nil || fd.Status != "M" {
		t.Fatalf("second diff = %+v, %v", fd, err)
	}
	if _, err := s.CommitFileDiff(bg, "--output=x", "f.txt", ""); err == nil {
		t.Fatal("option-like hash accepted")
	}
	if msg, err := s.LastCommitMessage(bg); err != nil || msg != "second\n\nbody" {
		t.Fatalf("LastCommitMessage = %q, %v", msg, err)
	}
	refs, err := s.Refs(bg)
	if err != nil || refs.Head.Branch != "main" {
		t.Fatalf("Refs = %+v, %v", refs, err)
	}
	st, err := s.Status(bg)
	if err != nil || len(st.Files) != 0 {
		t.Fatalf("Status = %+v, %v", st, err)
	}

	// A commit made outside the session is readable once refs changed.
	third := repo.CommitAll("third")
	s.refsChanged()
	if d, err := s.Commit(bg, third); err != nil || d.Summary != "third" {
		t.Fatalf("Commit after external commit = %+v, %v", d, err)
	}
}

func TestSentinelErrorsPropagate(t *testing.T) {
	repo := testutil.NewRepo(t)
	s, _ := open(t, repo)
	if err := s.StashPush(bg, "", false); !errors.Is(err, ops.ErrStashUnborn) {
		t.Fatalf("stash on unborn: err = %v", err)
	}
	if _, err := s.CommitChanges(bg, ops.CommitOptions{Message: "x", Amend: true}); !errors.Is(err, ops.ErrNothingToAmend) {
		t.Fatalf("amend on unborn: err = %v", err)
	}
	repo.CommitAll("one")
	if _, err := s.CommitChanges(bg, ops.CommitOptions{Message: "  "}); !errors.Is(err, ops.ErrEmptyMessage) {
		t.Fatalf("empty message: err = %v", err)
	}
	if _, err := s.CommitChanges(bg, ops.CommitOptions{Message: "x"}); !errors.Is(err, ops.ErrNothingToCommit) {
		t.Fatalf("nothing to commit: err = %v", err)
	}
	if err := s.StashPush(bg, "", false); !errors.Is(err, ops.ErrNothingToStash) {
		t.Fatalf("nothing to stash: err = %v", err)
	}
}

func TestStashByHash(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\n")
	repo.CommitAll("base")
	s, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(1))

	repo.Write("f.txt", "first\n")
	if err := s.StashPush(bg, "first", false); err != nil {
		t.Fatal(err)
	}
	repo.Write("f.txt", "second\n")
	if err := s.StashPush(bg, "second", false); err != nil {
		t.Fatal(err)
	}
	// The stash rows are part of the graph after a reload.
	rec.waitFor(t, "graph:progress", graphDone(3))
	st, err := s.Stashes(bg)
	if err != nil || len(st) != 2 {
		t.Fatalf("Stashes = %+v, %v", st, err)
	}
	first := st[1].Hash // stash@{1}
	if err := s.StashDrop(bg, st[0].Hash); err != nil {
		t.Fatal(err)
	}
	// first is now stash@{0}: the hash still targets it.
	if err := s.StashApply(bg, first); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("show", ":f.txt"); got != "a" {
		t.Fatalf("index after apply = %q", got)
	}
	repo.Git("checkout", "--", "f.txt")
	if err := s.StashPop(bg, first); err != nil {
		t.Fatal(err)
	}
	if err := s.StashDrop(bg, first); !errors.Is(err, ops.ErrStashNotFound) {
		t.Fatalf("drop of a popped stash: err = %v", err)
	}
}

func TestBranchAndTagOps(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "origin", bare)
	repo.Git("push", "-q", "origin", "main")
	s, _ := open(t, repo)
	if err := s.CreateBranch(bg, "feat", "main", true); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameBranch(bg, "feat", "feat2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Checkout(bg, ops.CheckoutTarget{Kind: "local", Name: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUpstream(bg, "main", "origin/main"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBranch(bg, "feat2", false); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTag(bg, "v1", "main", "release"); err != nil {
		t.Fatal(err)
	}
	if err := s.PushTag(bg, "origin", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTag(bg, "v1"); err != nil {
		t.Fatal(err)
	}
	repo.Git("push", "-q", "origin", "main:gone")
	if err := s.DeleteRemoteBranch(bg, "origin", "gone"); err != nil {
		t.Fatal(err)
	}
	if out := repo.Git("ls-remote", "origin"); len(out) == 0 || contains(out, "refs/heads/gone") || !contains(out, "refs/tags/v1") {
		t.Fatalf("ls-remote = %q", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestStageUnstageDiscard(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("one")
	s, _ := open(t, repo)
	repo.Write("a.txt", "2\n")
	repo.Write("u.txt", "u\n")
	if err := s.Stage(bg, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Unstage(bg, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UnstageAll(bg); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(bg, []string{"a.txt"}, []string{"u.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("status", "--porcelain"); got != "" {
		t.Fatalf("status after discard = %q", got)
	}
}

// A second remote operation while one is running is refused; CancelOp aborts the
// running one, even while it is still waiting for the write lock.
func TestRemoteOpExclusiveAndCancel(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	repo.Git("remote", "add", "origin", testutil.NewBare(t, repo.R))
	s, _ := open(t, repo)

	s.writeSem <- struct{}{} // hold the write lock so the fetch stays in flight
	errc := make(chan error, 1)
	go func() { errc <- s.Fetch() }()
	deadline := time.Now().Add(5 * time.Second)
	for !s.opRunning() {
		if time.Now().After(deadline) {
			t.Fatal("fetch did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.Pull(ops.PullDefault); !errors.Is(err, ErrOpInProgress) {
		t.Fatalf("second remote op: err = %v", err)
	}
	s.CancelOp()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled fetch: err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CancelOp did not abort the fetch")
	}
	<-s.writeSem
	if err := s.Fetch(); err != nil {
		t.Fatalf("fetch after cancel: %v", err)
	}
}

func TestOpProgressEmitted(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("big.bin", string(make([]byte, 64<<10)))
	repo.CommitAll("one")
	repo.Git("remote", "add", "origin", testutil.NewBare(t, repo.R))
	s, rec := open(t, repo)
	if err := s.PushCurrent(false); err != nil {
		t.Fatal(err)
	}
	// git only reports progress after a delay, so progress events are not
	// guaranteed on a tiny push; when present they must be well-formed.
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for i, e := range rec.events {
		if e == "op:progress" {
			if p := rec.data[i].(OpProgress); p.Tab != "t1" || p.Op != "push" {
				t.Fatalf("op:progress = %+v", p)
			}
		}
	}
}

func TestCloseStopsEvents(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	s, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(1))
	s.Close()
	s.Close() // idempotent
	n := rec.total()
	s.Reload()
	repo.Write("x.txt", "x\n")
	if err := s.StageAll(bg); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after Close: err = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if rec.total() != n {
		t.Fatal("events emitted after Close")
	}
}

// A linked worktree has its git dir outside the worktree: both are watched.
func TestLinkedWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("fsevents")
	}
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	wt := filepath.Join(t.TempDir(), "wt")
	repo.Git("worktree", "add", "-q", "-b", "side", wt)
	rec := newRecorder()
	s, err := Open("t2", wt, repo.R, rec.emit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if real, _ := filepath.EvalSymlinks(wt); s.Dir != real {
		t.Fatalf("Dir = %q, want %q", s.Dir, real)
	}
	rec.waitFor(t, "graph:progress", graphDone(1))
	time.Sleep(300 * time.Millisecond)
	// An external commit in the linked worktree updates refs/heads/side in the
	// common git dir, outside both the worktree and its private git dir.
	if _, err := repo.R.Run(bg, gitexec.Cmd{Dir: wt, Args: []string{"commit", "-q", "--allow-empty", "-m", "side commit"}}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, "graph:progress", graphDone(2))
}
