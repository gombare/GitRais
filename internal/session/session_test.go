package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gitrais/internal/diff"
	"gitrais/internal/gitexec"
	"gitrais/internal/gitread"
	"gitrais/internal/graph"
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

// count2 is count for callers already holding r.mu.
func (r *recorder) count2(ev string) int {
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
	if err := s.StageLines(bg, StageLinesRequest{Path: "f.txt", Hunk: 0, Lines: []int{99}}); !errors.Is(err, ErrLineOutOfRange) {
		t.Fatalf("out-of-range line: err = %v", err)
	}
	repo.Write("new.txt", "x\n")
	repo.Git("add", "new.txt")
	if err := s.StageLines(bg, StageLinesRequest{Path: "new.txt", Staged: true, Hunk: 0}); !errors.Is(err, ErrPartialStageUnsupported) {
		t.Fatalf("added file: err = %v", err)
	}
}

// fingerprint builds the Expect of a StageLinesRequest from a loaded diff.
func fingerprint(fd *diff.FileDiff, hunk int, lines []int) *HunkFingerprint {
	h := fd.Hunks[hunk]
	fp := &HunkFingerprint{OldStart: h.OldStart, OldLines: h.OldLines, NewStart: h.NewStart, NewLines: h.NewLines}
	idx := lines
	if len(idx) == 0 {
		for i := range h.Lines {
			idx = append(idx, i)
		}
	}
	for _, i := range idx {
		fp.Lines = append(fp.Lines, FingerprintLine{Kind: string(h.Lines[i].Kind), Text: h.Lines[i].Text})
	}
	return fp
}

func TestStageLinesFingerprint(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\nb\nc\n")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\nB\nc\nd\n")
	s, _ := open(t, repo)
	fd, err := s.WipDiff(bg, ops.WipDiffRequest{Path: "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	addB := -1
	for i, l := range fd.Hunks[0].Lines {
		if l.Kind == "+" && l.Text == "B" {
			addB = i
		}
	}
	// The file changes after the diff was loaded: same hunk shape, different text.
	repo.Write("f.txt", "a\nX\nc\nd\n")
	before := repo.Git("ls-files", "-s", "f.txt")
	err = s.StageLines(bg, StageLinesRequest{Path: "f.txt", Hunk: 0, Lines: []int{addB}, Expect: fingerprint(&fd, 0, []int{addB})})
	if !errors.Is(err, ErrDiffChanged) {
		t.Fatalf("changed file: err = %v", err)
	}
	if err := s.StageLines(bg, StageLinesRequest{Path: "f.txt", Hunk: 0, Expect: fingerprint(&fd, 0, nil)}); !errors.Is(err, ErrDiffChanged) {
		t.Fatalf("changed file, whole hunk: err = %v", err)
	}
	if got := repo.Git("ls-files", "-s", "f.txt"); got != before {
		t.Fatalf("index changed: %q -> %q", before, got)
	}
	// A fresh load with a matching fingerprint stages.
	fd, err = s.WipDiff(bg, ops.WipDiffRequest{Path: "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	addX := -1
	for i, l := range fd.Hunks[0].Lines {
		if l.Kind == "+" && l.Text == "X" {
			addX = i
		}
	}
	if err := s.StageLines(bg, StageLinesRequest{Path: "f.txt", Hunk: 0, Lines: []int{addX}, Expect: fingerprint(&fd, 0, []int{addX})}); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("diff", "--cached", "--numstat"); got != "1\t0\tf.txt" {
		t.Fatalf("staged = %q", got)
	}
	// A fingerprint with a different line count is a mismatch too.
	fp := fingerprint(&fd, 0, nil)
	fp.Lines = fp.Lines[:1]
	if err := s.StageLines(bg, StageLinesRequest{Path: "f.txt", Hunk: 0, Expect: fp}); !errors.Is(err, ErrDiffChanged) {
		t.Fatalf("short fingerprint: err = %v", err)
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

	// Concurrent detail reads run under -race.
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
	if v, err := s.FileVersions(bg, ops.VersionsRequest{Path: "f.txt", Commit: second}); err != nil || v.Old != "a\n" || v.New != "a\nb\n" {
		t.Fatalf("FileVersions = %+v, %v", v, err)
	}
	if _, err := s.FileVersions(bg, ops.VersionsRequest{Path: "f.txt", Commit: "HEAD"}); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("FileVersions(HEAD) err = %v", err)
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
	// GIT_PROGRESS_DELAY=0 makes git report delayed progress meters at once, so
	// even a tiny local push and fetch produce progress lines.
	r := testutil.Runner(t)
	r.Env = append(r.Env, "GIT_PROGRESS_DELAY=0")
	repo := testutil.NewRepoWith(t, r)
	repo.Write("a.txt", "a\n")
	repo.Write("b.txt", "b\n")
	repo.CommitAll("one")
	bare := testutil.NewBare(t, r)
	repo.Git("remote", "add", "origin", bare)
	s, rec := open(t, repo)
	if err := s.PushCurrent(false); err != nil {
		t.Fatal(err)
	}
	// A second clone pushes new objects, which the session then fetches.
	other := testutil.NewRepoWith(t, r)
	other.Git("pull", "-q", bare, "main")
	other.Write("c.txt", "c\n")
	other.CommitAll("two")
	other.Git("push", "-q", bare, "main")
	if err := s.Fetch(); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	got := map[string]int{}
	for i, e := range rec.events {
		if e == "op:progress" {
			p := rec.data[i].(OpProgress)
			if p.Tab != "t1" || p.Phase == "" || p.Total <= 0 {
				t.Fatalf("op:progress = %+v", p)
			}
			got[p.Op]++
		}
	}
	if got["push"] == 0 || got["fetch"] == 0 {
		t.Fatalf("op:progress per op = %v, want push and fetch progress", got)
	}
}

// A superseded graph loader must not emit anything after the newer loader's
// graph:reset, even when it was already past the swap.
func TestStaleLoaderEventsDropped(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	beforeGraphProgress = func(gen uint64) {
		if gen == 1 {
			once.Do(func() { close(reached); <-release })
		}
	}
	rec := newRecorder()
	s, err := Open("t1", repo.Dir, repo.R, rec.emit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); beforeGraphProgress = nil })
	<-reached // generation 1 is installed and about to publish its progress
	s.Reload()
	rec.waitFor(t, "graph:progress", graphDone(1))
	close(release)
	s.loadWG.Wait() // generation 1 finished: anything it emitted is recorded

	rec.mu.Lock()
	defer rec.mu.Unlock()
	last := -1
	for i, e := range rec.events {
		if e == "graph:reset" {
			last = i
		}
	}
	if rec.count2("graph:reset") != 2 {
		t.Fatalf("events = %v, want two graph:reset", rec.events)
	}
	// Only generation 2's progress and done may follow its reset.
	if after := rec.events[last+1:]; len(after) != 2 {
		t.Fatalf("events after the last graph:reset = %v", after)
	}
}

// A stash read failure must not blank the graph: it loads without stash rows.
func TestStashReadFailureStillLoadsGraph(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	repo.CommitAll("two")
	readStashes = func(context.Context, *gitexec.Runner, string) ([]graph.Stash, error) {
		return nil, errors.New("stash list failed")
	}
	t.Cleanup(func() { readStashes = gitread.ReadStashes })
	s, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(2))
	if n, done := s.GraphInfo(); n != 2 || !done {
		t.Fatalf("GraphInfo = %d, %v", n, done)
	}
}

// slowPreCommit installs a pre-commit hook that touches a marker and then
// sleeps for the given shell duration; it returns the marker path.
func slowPreCommit(t *testing.T, repo *testutil.Repo, sleep string) string {
	t.Helper()
	hooks := filepath.Join(repo.Dir, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "started")
	hook := "#!/bin/sh\ntouch '" + marker + "'\nsleep " + sleep + "\n"
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	return marker
}

func waitFile(t *testing.T, p string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(p); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear", p)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Close lets a running local write finish (it is not killed half-way) and
// waits for it, without emitting anything once Close has started.
func TestCloseLetsRunningLocalWriteFinish(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	marker := slowPreCommit(t, repo, "1")
	s, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(1))
	repo.Write("x.txt", "x\n")
	repo.Git("add", "x.txt")
	errc := make(chan error, 1)
	go func() {
		_, err := s.CommitChanges(bg, ops.CommitOptions{Message: "x"})
		errc <- err
	}()
	waitFile(t, marker)
	n := rec.total()
	s.Close()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("write running at Close: err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close returned before the running write")
	}
	if c := repo.Git("rev-list", "--count", "HEAD"); c != "2" {
		t.Fatalf("commits = %s, the write did not land", c)
	}
	time.Sleep(100 * time.Millisecond)
	if rec.total() != n {
		t.Fatalf("events emitted during or after Close: %v", rec.events[n:])
	}
}

// A local write still running after the grace period is cancelled.
func TestCloseCancelsLocalWriteAfterGrace(t *testing.T) {
	old := closeGrace
	closeGrace = 300 * time.Millisecond
	t.Cleanup(func() { closeGrace = old })
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	marker := slowPreCommit(t, repo, "30")
	s, _ := open(t, repo)
	repo.Write("x.txt", "x\n")
	repo.Git("add", "x.txt")
	errc := make(chan error, 1)
	go func() {
		_, err := s.CommitChanges(bg, ops.CommitOptions{Message: "x"})
		errc <- err
	}()
	waitFile(t, marker)
	start := time.Now()
	s.Close()
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("Close took %v", d)
	}
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("write cancelled by Close returned nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write still running after Close returned")
	}
	if c := repo.Git("rev-list", "--count", "HEAD"); c != "1" {
		t.Fatalf("commits = %s, the cancelled commit landed", c)
	}
}

// A hanging remote operation is cancelled at once by Close.
func TestCloseCancelsRemoteOpFast(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	marker := filepath.Join(t.TempDir(), "ssh")
	// GIT_SSH_COMMAND, not core.sshCommand: the go tool exports its own
	// GIT_SSH_COMMAND to tests, which would win over the config.
	repo.R.Env = append(repo.R.Env, "GIT_SSH_COMMAND=touch '"+marker+"'; sleep 30; false")
	repo.Git("remote", "add", "origin", "ssh://example.invalid/x.git")
	s, _ := open(t, repo)
	errc := make(chan error, 1)
	go func() { errc <- s.Fetch() }()
	waitFile(t, marker)
	start := time.Now()
	s.Close()
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Close took %v with a hanging fetch", d)
	}
	if err := <-errc; err == nil {
		t.Fatal("fetch cancelled by Close returned nil")
	}
}

// A write still waiting for the write lock when Close runs never starts.
func TestCloseRejectsQueuedWrite(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	s, _ := open(t, repo)
	s.writeSem <- struct{}{} // a write in progress holds the lock
	repo.Write("x.txt", "x\n")
	errc := make(chan error, 1)
	go func() { errc <- s.StageAll(bg) }()
	time.Sleep(100 * time.Millisecond)
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("queued write: err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued write not released by Close")
	}
	<-s.writeSem
	<-done
	if st := repo.Git("status", "--porcelain"); st != "?? x.txt" {
		t.Fatalf("status = %q, the queued write ran", st)
	}
}

func TestPushCurrentAfterClose(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	repo.Git("remote", "add", "origin", testutil.NewBare(t, repo.R))
	s, _ := open(t, repo)
	s.Close()
	if err := s.PushCurrent(false); !errors.Is(err, ErrClosed) {
		t.Fatalf("PushCurrent after Close: err = %v", err)
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

func TestStageLinesFingerprintNonUTF8(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("lat.txt", "a\nb\xe9\nc\n")
	repo.CommitAll("base")
	repo.Write("lat.txt", "a\nB\xe9\nc\n")
	s, _ := open(t, repo)
	fd, err := s.WipDiff(bg, ops.WipDiffRequest{Path: "lat.txt"})
	if err != nil {
		t.Fatal(err)
	}
	// The fingerprint reaches the backend through JSON, as over the Wails bridge.
	raw, _ := json.Marshal(fingerprint(&fd, 0, nil))
	var fp HunkFingerprint
	if err := json.Unmarshal(raw, &fp); err != nil {
		t.Fatal(err)
	}
	if err := s.StageLines(bg, StageLinesRequest{Path: "lat.txt", Hunk: 0, Expect: &fp}); err != nil {
		t.Fatalf("whole hunk of a Latin-1 file: %v", err)
	}
}

// Repositories with extensions go-git rejects (worktreeConfig after a sparse
// checkout, sha256 object format) open and their commits are readable.
func TestOpenRepoWithExtensions(t *testing.T) {
	r := testutil.Runner(t)
	sparse := testutil.NewRepoWith(t, r)
	sparse.Write("x/a.txt", "a\n")
	sparse.Write("y/b.txt", "b\n")
	sparse.CommitAll("base")
	sparse.Git("sparse-checkout", "set", "x")

	shaDir := filepath.Join(t.TempDir(), "sha256")
	if _, err := r.Run(bg, gitexec.Cmd{Args: []string{"init", "-q", "-b", "main", "--object-format=sha256", shaDir}}); err != nil {
		t.Fatal(err)
	}
	sha := &testutil.Repo{Dir: shaDir, R: r}
	sha.Dir, _ = filepath.EvalSymlinks(shaDir)
	if _, err := r.Run(bg, gitexec.Cmd{Dir: sha.Dir, Args: []string{"commit", "-q", "--allow-empty", "-m", "sha root"}}); err != nil {
		t.Fatal(err)
	}

	for _, repo := range []*testutil.Repo{sparse, sha} {
		s, rec := open(t, repo)
		rec.waitFor(t, "graph:progress", graphDone(1))
		head := rows0(s)
		d, err := s.Commit(bg, head)
		if err != nil || d.Hash != head {
			t.Fatalf("%s: Commit = %+v, %v", repo.Dir, d, err)
		}
	}
}

// TestMain disables the background commit-graph write by default: it reloads
// the graph, which would add events to tests that count them.
func TestMain(m *testing.M) {
	autoCommitGraph = false
	defaultAutoFetchInterval = 0 // tests enable it explicitly
	os.Exit(m.Run())
}

func enableCommitGraph(t *testing.T) {
	autoCommitGraph = true
	t.Cleanup(func() { autoCommitGraph = false })
}

// Opening a repository without a commit-graph writes one in the background
// and reloads the graph once it is there.
func TestOpenWritesCommitGraph(t *testing.T) {
	enableCommitGraph(t)
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	repo.CommitAll("two")
	info := filepath.Join(repo.Dir, ".git", "objects", "info")
	for _, p := range []string{"commit-graph", "commit-graphs"} {
		if _, err := os.Stat(filepath.Join(info, p)); err == nil {
			t.Fatalf("%s already exists", p)
		}
	}
	s, rec := open(t, repo)
	deadline := time.Now().Add(10 * time.Second)
	for rec.count("graph:reset") < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("no reload after the commit-graph write: %v", rec.events)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(info, "commit-graphs", "commit-graph-chain")); err != nil {
		t.Fatalf("commit-graph not written: %v", err)
	}
	rec.waitFor(t, "graph:progress", graphDone(2))
	if n, _ := s.GraphInfo(); n != 2 {
		t.Fatalf("rows = %d", n)
	}

	// Already present: a second session does not write (nor reload) again.
	rec2 := newRecorder()
	s2, err := Open("t2", repo.Dir, repo.R, rec2.emit)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	rec2.waitFor(t, "graph:progress", graphDone(2))
	time.Sleep(300 * time.Millisecond)
	if n := rec2.count("graph:reset"); n != 1 {
		t.Fatalf("graph:reset = %d with an existing commit-graph", n)
	}
}

// Close cancels a running commit-graph write and waits for it.
func TestCloseCancelsCommitGraphWrite(t *testing.T) {
	enableCommitGraph(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	writeCommitGraph = func(ctx context.Context, r *gitexec.Runner, dir string) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	}
	t.Cleanup(func() { writeCommitGraph = runCommitGraphWrite })
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	s, rec := open(t, repo)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("commit-graph write not started")
	}
	start := time.Now()
	s.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Close took %v", d)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("Close returned before the commit-graph write ended")
	}
	if n := rec.count("graph:reset"); n > 1 {
		t.Fatalf("reload after a cancelled commit-graph write: %d resets", n)
	}
}

// Ref changes made by someone else while a working-tree-only write runs are
// dropped by the suspended watcher; the session still reports them.
func TestRefChangeDuringWipWriteNotLost(t *testing.T) {
	if testing.Short() {
		t.Skip("fsevents")
	}
	repo := testutil.NewRepo(t)
	repo.CommitAll("one")
	s, rec := open(t, repo)
	rec.waitFor(t, "graph:progress", graphDone(1))
	time.Sleep(300 * time.Millisecond)
	before := rec.count("refs:changed")
	err := s.write(bg, false, func(context.Context) error {
		repo.CommitAll("external commit") // e.g. a commit from a terminal
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := rec.count("refs:changed"); n <= before {
		t.Fatalf("refs:changed not emitted after a wip-only write that saw a ref change: %v", rec.events)
	}
	rec.waitFor(t, "graph:progress", graphDone(2))
}

// PullAutostash goes through the remote path: local changes the incoming
// commit also touches survive, and refs:changed follows.
func TestPullAutostash(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "1\n2\n3\n4\n5\n6\n")
	repo.CommitAll("one")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "origin", bare)
	repo.Git("push", "-q", "-u", "origin", "main")
	other := testutil.NewRepoWith(t, repo.R)
	other.Git("pull", "-q", bare, "main")
	other.Write("f.txt", "remote\n2\n3\n4\n5\n6\n")
	other.CommitAll("two")
	other.Git("push", "-q", bare, "main")
	repo.Write("f.txt", "1\n2\n3\n4\n5\nlocal\n")
	s, rec := open(t, repo)
	if err := s.Pull(ops.PullDefault); gitexec.KindOf(err) != gitexec.KindDirtyWorktree {
		t.Fatalf("plain pull: err = %v", err)
	}
	before := rec.count(EventRefsChanged)
	if err := s.PullAutostash(ops.PullDefault); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(repo.Path("f.txt")); string(b) != "remote\n2\n3\n4\n5\nlocal\n" {
		t.Fatalf("f.txt = %q", b)
	}
	if rec.count(EventRefsChanged) <= before {
		t.Fatal("refs:changed not emitted")
	}
}
