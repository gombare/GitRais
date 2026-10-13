package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gitrais/internal/askpass"
	"gitrais/internal/gitexec"
	"gitrais/internal/graph"
	"gitrais/internal/ops"
	"gitrais/internal/session"
	"gitrais/internal/store"
	"gitrais/internal/testutil"
)

type events struct {
	mu  sync.Mutex
	got []string
	pay []any
}

func (e *events) emit(name string, d any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got, e.pay = append(e.got, name), append(e.pay, d)
}

func (e *events) find(name string) (any, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, n := range e.got {
		if n == name {
			return e.pay[i], true
		}
	}
	return nil, false
}

// all returns the payloads of every event with the given name, in order.
func (e *events) all(name string) []any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []any
	for i, n := range e.got {
		if n == name {
			out = append(out, e.pay[i])
		}
	}
	return out
}

// waitN polls until at least n events with the given name were emitted.
func (e *events) waitN(t *testing.T, name string, n int) []any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := e.all(name); len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d %s event(s) not emitted; got %v", n, name, e.all(name))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newTestApp(t *testing.T) (*App, *events, *testutil.Repo) {
	t.Helper()
	repo := testutil.NewRepo(t)
	dir := t.TempDir()
	a, err := NewWithConfig(Config{StatePath: filepath.Join(dir, "state.json"), LogPath: filepath.Join(dir, "a.log"), Git: repo.R.Git})
	if err != nil {
		t.Fatal(err)
	}
	a.runner.Env = append(a.runner.Env, repo.R.Env...)
	ev := &events{}
	a.emitFn = ev.emit
	t.Cleanup(func() { a.shutdown() })
	return a, ev, repo
}

func decodeUIError(t *testing.T, err error) UIError {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var ue UIError
	if jerr := json.Unmarshal([]byte(err.Error()), &ue); jerr != nil {
		t.Fatalf("error is not UIError JSON: %q (%v)", err.Error(), jerr)
	}
	return ue
}

func waitGraph(t *testing.T, a *App, tab string, rows int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		gi, _ := a.GraphInfo(tab)
		if gi.Done && gi.Rows == rows {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("graph not loaded: %+v", gi)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestVersion(t *testing.T) {
	if New().Version() != "0.1.0" {
		t.Fatal("version")
	}
}

func TestOpenRepoAndRead(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("first")
	tab, err := a.OpenRepo(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if tab.Name != filepath.Base(repo.Dir) {
		t.Fatalf("tab = %+v", tab)
	}
	waitGraph(t, a, tab.ID, 1)
	rows, err := a.Rows(tab.ID, 0, 10)
	if err != nil || len(rows) != 1 || rows[0].Kind != "commit" || rows[0].Subject != "first" {
		t.Fatalf("rows = %+v %v", rows, err)
	}
	st, _ := a.GetState()
	if len(st.Tabs) != 1 || st.ActiveTab != tab.ID || st.Recents[0] != tab.Path {
		t.Fatalf("state = %+v", st)
	}
	again, _ := a.OpenRepo(repo.Dir)
	if again.ID != tab.ID {
		t.Fatal("reopening the same repo must return the existing tab")
	}
	// A subdirectory resolves to the same repository: still the same tab.
	repo.Write("sub/b.txt", "x\n")
	sub, err := a.OpenRepo(filepath.Join(repo.Dir, "sub"))
	if err != nil || sub.ID != tab.ID {
		t.Fatalf("subdir tab = %+v %v", sub, err)
	}
	if st, _ := a.GetState(); len(st.Tabs) != 1 {
		t.Fatalf("tabs after reopen = %+v", st.Tabs)
	}
	if i, err := a.FindRow(tab.ID, rows[0].Hash); err != nil || i != 0 {
		t.Fatalf("FindRow = %d %v", i, err)
	}
}

func TestRowsDTO(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("first")
	repo.Write("a.txt", "2\n")
	repo.Git("stash", "push", "-m", "wip")
	tab, err := a.OpenRepo(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	waitGraph(t, a, tab.ID, 2)
	rows, err := a.Rows(tab.ID, 0, 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v %v", rows, err)
	}
	stash, commit := rows[0], rows[1]
	if stash.Kind != "stash" || stash.StashRef != "stash@{0}" || len(stash.Parents) != 1 || stash.Parents[0] != commit.Hash {
		t.Fatalf("stash row = %+v", stash)
	}
	if commit.Kind != "commit" || commit.Author != "Test" || commit.Email != "test@example.com" || commit.Time == 0 {
		t.Fatalf("commit row = %+v", commit)
	}
	if commit.Parents == nil || commit.Up == nil || commit.Down == nil {
		t.Fatalf("nil slices would marshal as null: %+v", commit)
	}
	b, _ := json.Marshal(commit)
	for _, k := range []string{`"hash"`, `"parents":[]`, `"kind":"commit"`, `"lane"`, `"color"`, `"up"`, `"down"`, `"stashRef"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("json %s lacks %s", b, k)
		}
	}
	stashes, err := a.Stashes(tab.ID)
	if err != nil || len(stashes) != 1 || stashes[0].Hash != stash.Hash {
		t.Fatalf("stashes = %+v %v", stashes, err)
	}
	// Stash bindings take the stash hash.
	if err := a.StashDrop(tab.ID, stash.Hash); err != nil {
		t.Fatal(err)
	}
	ue := decodeUIError(t, a.StashDrop(tab.ID, stash.Hash))
	if ue.Kind != "StashNotFound" {
		t.Fatalf("second drop = %+v", ue)
	}
}

func TestToRowDTOSegments(t *testing.T) {
	d := toRowDTO(graph.Row{Hash: "h", Kind: graph.RowStash, StashRef: "stash@{1}",
		Up: []graph.Seg{{From: 1, To: 2, Color: 3}}, Down: []graph.Seg{{From: 0, To: 0, Color: 7}}})
	if d.Kind != "stash" || d.Up[0] != [3]int{1, 2, 3} || d.Down[0] != [3]int{0, 0, 7} || d.Parents == nil {
		t.Fatalf("dto = %+v", d)
	}
}

func TestUIErrorJSON(t *testing.T) {
	a, _, repo := newTestApp(t)
	tab, _ := a.OpenRepo(repo.Dir)
	_, err := a.Commit(tab.ID, ops.CommitOptions{Message: ""})
	if ue := decodeUIError(t, err); ue.Kind != "EmptyMessage" {
		t.Fatalf("err = %v", err)
	}
	_, err = a.Status("missing")
	if ue := decodeUIError(t, err); ue.Kind != "NoTab" {
		t.Fatalf("err = %v", err)
	}
	_, err = a.OpenRepo(t.TempDir())
	ue := decodeUIError(t, err)
	if ue.Kind != string(gitexec.KindNotARepo) || !strings.Contains(ue.Stderr, "not a git repository") {
		t.Fatalf("err = %v", err)
	}
	_, err = a.CommitDetail(tab.ID, "--output=/tmp/x")
	if ue := decodeUIError(t, err); ue.Kind != "InvalidHash" {
		t.Fatalf("err = %v", err)
	}
	_, err = a.FileVersions(tab.ID, ops.VersionsRequest{Path: "../x", Untracked: true})
	if ue := decodeUIError(t, err); ue.Kind != "InvalidFilePath" {
		t.Fatalf("err = %v", err)
	}
}

func TestUIErrorKinds(t *testing.T) {
	cases := []struct {
		err  error
		kind string
	}{
		{ops.ErrEmptyMessage, "EmptyMessage"},
		{fmt.Errorf("commit: %w", ops.ErrNothingToCommit), "NothingToCommit"},
		{ops.ErrNothingToAmend, "NothingToAmend"},
		{ops.ErrNothingToStash, "NothingToStash"},
		{ops.ErrStashUnborn, "StashUnborn"},
		{ops.ErrStashNotFound, "StashNotFound"},
		{fmt.Errorf("pull: %w", ops.ErrPullStrategyRequired), "PullStrategyRequired"},
		{ops.ErrAutostashConflict, "AutostashConflict"},
		{ops.ErrSwitchedNotUpdated, "SwitchedNotUpdated"},
		{ops.ErrDestNotEmpty, "DestNotEmpty"},
		{ops.ErrLockInUse, "LockInUse"},
		{ops.ErrLockTooRecent, "LockTooRecent"},
		{ops.ErrLsofMissing, "LsofMissing"},
		{ops.ErrGitRunning, "GitRunning"},
		{fmt.Errorf("%w: x", ops.ErrInvalidFilePath), "InvalidFilePath"},
		{session.ErrClosed, "Cancelled"},
		{context.Canceled, "Cancelled"},
		{context.DeadlineExceeded, "Timeout"},
		{session.ErrOpInProgress, "OpInProgress"},
		{session.ErrInvalidHash, "InvalidHash"},
		{session.ErrPartialStageUnsupported, "PartialStageUnsupported"},
		{session.ErrHunkOutOfRange, "HunkOutOfRange"},
		{session.ErrDiffChanged, "DiffChanged"},
		{session.ErrLineOutOfRange, "LineOutOfRange"},
		{session.ErrNoLinesSelected, "NoLinesSelected"},
		{session.ErrNoBranch, "NoBranch"},
		{session.ErrNoRemote, "NoRemote"},
		{session.ErrUpstreamNotRemote, "UpstreamNotRemote"},
		{fmt.Errorf("%w: bad json", store.ErrCorrupt), "StateCorrupt"},
		{errNoTab, "NoTab"},
		{errGitMissing, "GitMissing"},
		{errors.New("boom"), "Unknown"},
	}
	for _, c := range cases {
		ue := decodeUIError(t, uiErr(c.err))
		if ue.Kind != c.kind || ue.Message != c.err.Error() || ue.Stderr != "" {
			t.Errorf("%v: got %+v, want kind %s", c.err, ue, c.kind)
		}
	}
	if uiErr(nil) != nil {
		t.Fatal("uiErr(nil) must be nil")
	}
	// Idempotent: converting twice keeps the original payload.
	once := uiErr(session.ErrNoBranch)
	if twice := uiErr(once); twice.Error() != once.Error() {
		t.Fatalf("double conversion: %s != %s", twice, once)
	}
}

// The switch-but-no-fast-forward sentinel wins over the git kind and keeps
// git's stderr.
func TestUIErrorSwitchedNotUpdatedKeepsStderr(t *testing.T) {
	ge := &gitexec.GitError{Args: []string{"merge"}, ExitCode: 1, Stderr: "error: untracked working tree files would be overwritten by merge",
		Kind: gitexec.KindUntrackedOverwrite}
	ue := decodeUIError(t, uiErr(fmt.Errorf("%w: %w", ops.ErrSwitchedNotUpdated, ge)))
	if ue.Kind != "SwitchedNotUpdated" || !strings.Contains(ue.Stderr, "would be overwritten") {
		t.Fatalf("got %+v", ue)
	}
}

func TestUIErrorGitErrorRedacted(t *testing.T) {
	a, _, _ := newTestApp(t)
	_, err := a.runner.Run(context.Background(), gitexec.Cmd{Dir: t.TempDir(),
		Args: []string{"ls-remote", "http://alice:s3cret@127.0.0.1:1/repo.git"}})
	if err == nil {
		t.Fatal("ls-remote against a closed port must fail")
	}
	ue := decodeUIError(t, uiErr(err))
	if ue.Kind != string(gitexec.KindOf(err)) || ue.Kind == "" || ue.Stderr == "" {
		t.Fatalf("ui error = %+v", ue)
	}
	if strings.Contains(err.Error(), "s3cret") || strings.Contains(uiErr(err).Error(), "s3cret") {
		t.Fatalf("credentials leaked: %s", uiErr(err))
	}
	// A plain error carrying a URL with credentials is redacted too.
	ue = decodeUIError(t, uiErr(errors.New("cannot reach https://bob:pw@example.com/x")))
	if strings.Contains(ue.Message, "pw@") {
		t.Fatalf("message not redacted: %q", ue.Message)
	}
}

func waitPrompt(t *testing.T, ev *events, n int) AuthPrompt {
	t.Helper()
	return ev.waitN(t, EventAuthPrompt, n)[n-1].(AuthPrompt)
}

func TestAuthRoundTrip(t *testing.T) {
	a, ev, _ := newTestApp(t)
	done := make(chan string)
	go func() {
		ans, ok := a.askHandler(t.Context(), "Password for 'https://u@h': ")
		if !ok {
			ans = "<cancel>"
		}
		done <- ans
	}()
	p := waitPrompt(t, ev, 1)
	if !p.Secret {
		t.Fatal("a password prompt must be secret")
	}
	a.AnswerAuth("unknown", "x", true) // ignored
	a.AnswerAuth(p.ID, "pw", true)
	if got := <-done; got != "pw" {
		t.Fatalf("answer = %q", got)
	}
	a.AnswerAuth(p.ID, "late", true) // no longer pending: must not block or panic
}

func TestAuthSecretHeuristic(t *testing.T) {
	for prompt, want := range map[string]bool{
		"Password for 'https://u@h': ":                  true,
		"Enter passphrase for key '/x/id_ed25519': ":    true,
		"Personal access TOKEN: ":                       true,
		"Username for 'https://github.com': ":           false,
		"Are you sure you want to continue connecting?": false,
	} {
		if isSecret(prompt) != want {
			t.Errorf("isSecret(%q) != %v", prompt, want)
		}
	}
}

func TestAuthPromptsSerialized(t *testing.T) {
	a, ev, _ := newTestApp(t)
	type res struct {
		ans string
		ok  bool
	}
	r1, r2 := make(chan res, 1), make(chan res, 1)
	go func() { s, ok := a.askHandler(t.Context(), "Username for 'https://h': "); r1 <- res{s, ok} }()
	first := waitPrompt(t, ev, 1)
	go func() { s, ok := a.askHandler(t.Context(), "Password for 'https://u@h': "); r2 <- res{s, ok} }()
	time.Sleep(100 * time.Millisecond)
	if n := len(ev.all(EventAuthPrompt)); n != 1 {
		t.Fatalf("second prompt shown while the first is pending: %d prompts", n)
	}
	if first.Secret {
		t.Fatal("username prompt must not be secret")
	}
	a.AnswerAuth(first.ID, "alice", true)
	if r := <-r1; r.ans != "alice" || !r.ok {
		t.Fatalf("first = %+v", r)
	}
	second := waitPrompt(t, ev, 2)
	if second.ID == first.ID || !second.Secret || !strings.HasPrefix(second.Prompt, "Password") {
		t.Fatalf("second = %+v", second)
	}
	a.AnswerAuth(first.ID, "stale", true) // answering a finished prompt is ignored
	a.AnswerAuth(second.ID, "", false)
	if r := <-r2; r.ok {
		t.Fatalf("second = %+v", r)
	}
	if c := ev.all(EventAuthCancel); len(c) != 0 {
		t.Fatalf("answered prompts must not emit auth:cancel: %v", c)
	}
}

func TestAuthCancelEvent(t *testing.T) {
	a, ev, _ := newTestApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	r1 := make(chan bool, 1)
	go func() { _, ok := a.askHandler(ctx, "Password: "); r1 <- ok }()
	first := waitPrompt(t, ev, 1)
	// A queued prompt whose helper dies before it is shown emits nothing.
	qctx, qcancel := context.WithCancel(t.Context())
	rq := make(chan bool, 1)
	go func() { _, ok := a.askHandler(qctx, "Queued password: "); rq <- ok }()
	r2 := make(chan string, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		s, _ := a.askHandler(t.Context(), "Passphrase: ")
		r2 <- s
	}()
	time.Sleep(150 * time.Millisecond)
	qcancel()
	if ok := <-rq; ok {
		t.Fatal("cancelled queued prompt must fail")
	}
	cancel() // the helper died / the op was cancelled
	if ok := <-r1; ok {
		t.Fatal("cancelled prompt must fail")
	}
	c := ev.waitN(t, EventAuthCancel, 1)
	if c[0].(AuthCancel).ID != first.ID {
		t.Fatalf("auth:cancel = %+v, want id %s", c, first.ID)
	}
	next := waitPrompt(t, ev, 2)
	if next.Prompt != "Passphrase: " {
		t.Fatalf("next prompt = %+v (the cancelled queued one must be skipped)", next)
	}
	a.AnswerAuth(next.ID, "pp", true)
	if s := <-r2; s != "pp" {
		t.Fatalf("answer = %q", s)
	}
	if n := len(ev.all(EventAuthCancel)); n != 1 {
		t.Fatalf("auth:cancel count = %d", n)
	}
}

func TestShutdownCancelsPendingPrompt(t *testing.T) {
	a, ev, _ := newTestApp(t)
	r := make(chan bool, 1)
	go func() { _, ok := a.askHandler(context.Background(), "Password: "); r <- ok }()
	waitPrompt(t, ev, 1)
	a.shutdown()
	select {
	case ok := <-r:
		if ok {
			t.Fatal("prompt must fail on shutdown")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending prompt not released by shutdown")
	}
}

func TestRestoreTabs(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.CommitAll("x")
	tab, _ := a.OpenRepo(repo.Dir)
	gone := testutil.NewRepo(t)
	goneTab, err := a.OpenRepo(gone.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetActiveTab(goneTab.ID); err != nil {
		t.Fatal(err)
	}
	a.shutdown()
	if err := os.RemoveAll(gone.Dir); err != nil {
		t.Fatal(err)
	}
	b, err := NewWithConfig(Config{StatePath: a.cfg.StatePath, LogPath: a.cfg.LogPath, Git: a.cfg.Git})
	if err != nil {
		t.Fatal(err)
	}
	b.runner.Env = append(b.runner.Env, repo.R.Env...)
	b.emitFn = func(string, any) {}
	defer b.shutdown()
	tabs, err := b.RestoreTabs()
	if err != nil || len(tabs) != 1 || tabs[0].ID != tab.ID || tabs[0].Path != repo.Dir {
		t.Fatalf("tabs = %+v %v", tabs, err)
	}
	st, _ := b.GetState()
	if len(st.Tabs) != 1 || st.ActiveTab != tab.ID {
		t.Fatalf("state after restore = %+v", st)
	}
	again, err := b.RestoreTabs() // idempotent: reuses open sessions
	if err != nil || len(again) != 1 || again[0].ID != tab.ID {
		t.Fatalf("second restore = %+v %v", again, err)
	}
	if _, err := b.Status(tab.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCloseTab(t *testing.T) {
	a, ev, repo := newTestApp(t)
	repo.CommitAll("x")
	other := testutil.NewRepo(t)
	t1, _ := a.OpenRepo(repo.Dir)
	t2, _ := a.OpenRepo(other.Dir)
	s, err := a.sess(t1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CloseTab(t1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Status(t1.ID); decodeUIError(t, err).Kind != "NoTab" {
		t.Fatalf("closed tab still reachable: %v", err)
	}
	st, _ := a.GetState()
	if len(st.Tabs) != 1 || st.Tabs[0].ID != t2.ID || st.ActiveTab != t2.ID {
		t.Fatalf("state = %+v", st)
	}
	// The session is closed (asynchronously) and no event of the closed tab
	// is forwarded after CloseTab returned.
	ev.mu.Lock()
	n := len(ev.got)
	ev.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for !errors.Is(s.StageAll(context.Background()), session.ErrClosed) {
		if time.Now().After(deadline) {
			t.Fatal("session not closed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	ev.mu.Lock()
	for i := n; i < len(ev.got); i++ {
		if b, _ := json.Marshal(ev.pay[i]); strings.Contains(string(b), t1.ID) {
			t.Errorf("event %s %s after CloseTab", ev.got[i], b)
		}
	}
	ev.mu.Unlock()
	if err := a.CloseTab("missing"); err != nil {
		t.Fatalf("closing an unknown tab = %v", err)
	}
}

func TestStateBindings(t *testing.T) {
	a, _, _ := newTestApp(t)
	st, err := a.ToggleFavorite("/r")
	if err != nil || len(st.Favorites) != 1 {
		t.Fatalf("fav = %+v %v", st, err)
	}
	if err := a.SaveDraft("/r", store.Draft{Summary: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveLayout(store.Layout{LeftWidth: 300, RightWidth: 400}); err != nil {
		t.Fatal(err)
	}
	st, _ = a.GetState()
	if st.Drafts["/r"].Summary != "s" || st.Layout.LeftWidth != 300 {
		t.Fatalf("state = %+v", st)
	}
	if err := a.SaveDraft("/r", store.Draft{}); err != nil {
		t.Fatal(err)
	}
	if st, _ = a.GetState(); len(st.Drafts) != 0 {
		t.Fatalf("empty draft not removed: %+v", st.Drafts)
	}
	if p, err := a.PickDirectory("x"); p != "" || err != nil {
		t.Fatalf("PickDirectory without runtime = %q %v", p, err)
	}
}

func TestShutdownClosesSessions(t *testing.T) {
	a, ev, repo := newTestApp(t)
	repo.CommitAll("x")
	tab, _ := a.OpenRepo(repo.Dir)
	s, _ := a.sess(tab.ID)
	start := time.Now()
	a.shutdown()
	if d := time.Since(start); d > shutdownTimeout {
		t.Fatalf("shutdown took %v", d)
	}
	if err := s.StageAll(context.Background()); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("session still open after shutdown: %v", err)
	}
	n := len(ev.all(session.EventWipChanged))
	repo.Write("late.txt", "x")
	time.Sleep(100 * time.Millisecond)
	if len(ev.all(session.EventWipChanged)) != n {
		t.Fatal("events after shutdown")
	}
	if _, err := a.OpenRepo(repo.Dir); decodeUIError(t, err).Kind != "Cancelled" {
		t.Fatalf("OpenRepo after shutdown = %v", err)
	}
	a.shutdown() // idempotent
}

func TestStartupProblems(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := NewWithConfig(Config{StatePath: statePath, LogPath: filepath.Join(dir, "a.log")})
	if err == nil || !errors.Is(err, store.ErrCorrupt) {
		t.Fatalf("err = %v", err)
	}
	defer a.shutdown()
	msg := a.StartupError()
	if !strings.Contains(msg, "corrupt") || !strings.Contains(strings.ToLower(msg), "git") {
		t.Fatalf("StartupError = %q", msg)
	}
	// The app still works for state, and repository bindings report GitMissing.
	if _, err := a.GetState(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.OpenRepo(dir); decodeUIError(t, err).Kind != "GitMissing" {
		t.Fatalf("OpenRepo without git = %v", err)
	}
}

func TestInitAndClone(t *testing.T) {
	a, ev, repo := newTestApp(t)
	repo.CommitAll("one")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "origin", bare)
	repo.Git("push", "-q", "origin", "main")

	dir := filepath.Join(t.TempDir(), "fresh")
	tab, err := a.InitRepo(dir)
	if err != nil || tab.ID == "" {
		t.Fatalf("init = %+v %v", tab, err)
	}

	parent := t.TempDir()
	ct, err := a.CloneRepo(bare, parent)
	if err != nil {
		t.Fatalf("clone = %v", err)
	}
	waitGraph(t, a, ct.ID, 1)
	for _, p := range ev.all(session.EventOpProgress) {
		if op := p.(session.OpProgress); op.Op == "clone" && op.Tab != "" {
			t.Fatalf("clone progress with tab: %+v", op)
		}
	}
	if want, _ := filepath.EvalSymlinks(filepath.Join(parent, "remote")); ct.Path != want {
		t.Fatalf("clone path = %q", ct.Path)
	}
	if _, err = a.CloneRepo(bare, "relative/dir"); decodeUIError(t, err).Kind != "InvalidPath" {
		t.Fatalf("relative parent = %v", err)
	}
	_, err = a.CloneRepo(bare, parent)
	if ue := decodeUIError(t, err); ue.Kind != "DestNotEmpty" {
		t.Fatalf("clone into non-empty dir = %+v", ue)
	}
	if err := a.CancelClone(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreTabsShutdownRaceKeepsSavedTabs(t *testing.T) {
	a, _, _ := newTestApp(t)
	var repos []*testutil.Repo
	for i := 0; i < 3; i++ {
		r := testutil.NewRepo(t)
		r.CommitAll("x")
		repos = append(repos, r)
		if _, err := a.OpenRepo(r.Dir); err != nil {
			t.Fatal(err)
		}
	}
	a.shutdown()
	b, err := NewWithConfig(Config{StatePath: a.cfg.StatePath, LogPath: a.cfg.LogPath, Git: a.cfg.Git})
	if err != nil {
		t.Fatal(err)
	}
	b.runner.Env = append(b.runner.Env, repos[0].R.Env...)
	b.emitFn = func(string, any) {}
	first := true
	b.beforeRestoreOpen = func(string) { // shutdown lands while restoring
		if first {
			first = false
			b.shutdown()
		}
	}
	if _, err := b.RestoreTabs(); err == nil || decodeUIError(t, err).Kind != "Cancelled" {
		t.Fatalf("RestoreTabs during shutdown = %v", err)
	}
	st, err := b.store.Load()
	if err != nil || len(st.Tabs) != 3 {
		t.Fatalf("saved tabs after raced restore = %+v %v", st.Tabs, err)
	}
}

func TestOpenRepoRejectsInvalidPaths(t *testing.T) {
	a, _, _ := newTestApp(t)
	for _, p := range []string{"", "relative/dir"} {
		if _, err := a.OpenRepo(p); decodeUIError(t, err).Kind != "InvalidPath" {
			t.Fatalf("OpenRepo(%q) = %v", p, err)
		}
	}
}

func TestRowsClampsRange(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.CommitAll("x")
	repo.Write("b.txt", "b")
	repo.CommitAll("y")
	tab, _ := a.OpenRepo(repo.Dir)
	waitGraph(t, a, tab.ID, 2)
	for _, c := range [][2]int{{1, math.MaxInt}, {0, math.MaxInt}, {-5, 1}, {0, -3}, {math.MaxInt, math.MaxInt}} {
		if _, err := a.Rows(tab.ID, c[0], c[1]); err != nil {
			t.Fatalf("Rows(%d,%d): %v", c[0], c[1], err)
		}
	}
	rows, _ := a.Rows(tab.ID, -5, 1)
	if len(rows) != 1 {
		t.Fatalf("negative start must act as 0: %d rows", len(rows))
	}
}

func TestShutdownClosesAskpassAfterSessions(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.CommitAll("x")
	tab, _ := a.OpenRepo(repo.Dir)
	s, _ := a.sess(tab.ID)
	ap, aerr := askpass.Start(a.askHandler)
	if aerr != nil {
		t.Fatal(aerr)
	}
	a.ap = ap
	var err error
	called := false
	a.beforeAskpassClose = func() {
		called = true
		err = s.StageAll(context.Background())
	}
	a.shutdown()
	if !called {
		t.Fatal("askpass close hook not called")
	}
	if !errors.Is(err, session.ErrClosed) {
		t.Fatalf("askpass server closed before the session: %v", err)
	}
}

func TestTabBindingsAfterShutdownAreCancelled(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.CommitAll("x")
	tab, _ := a.OpenRepo(repo.Dir)
	a.shutdown()
	if _, err := a.Status(tab.ID); decodeUIError(t, err).Kind != "Cancelled" {
		t.Fatalf("Status after shutdown = %v", err)
	}
	if err := a.Stage(tab.ID, nil); decodeUIError(t, err).Kind != "Cancelled" {
		t.Fatalf("Stage after shutdown = %v", err)
	}
}

func TestAuthTimeoutStartsWhenShown(t *testing.T) {
	a, ev, _ := newTestApp(t)
	a.authTimeoutOverride = 400 * time.Millisecond
	type res struct{ ok bool }
	r1, r2 := make(chan res, 1), make(chan res, 1)
	go func() { _, ok := a.askHandler(t.Context(), "Username: "); r1 <- res{ok} }()
	first := waitPrompt(t, ev, 1)
	go func() { _, ok := a.askHandler(t.Context(), "Password: "); r2 <- res{ok} }()
	time.Sleep(250 * time.Millisecond)
	a.AnswerAuth(first.ID, "u", true)
	<-r1
	second := waitPrompt(t, ev, 2)
	time.Sleep(250 * time.Millisecond) // 500 ms since queued, 250 ms since shown
	a.AnswerAuth(second.ID, "pw", true)
	select {
	case r := <-r2:
		if !r.ok {
			t.Fatal("queued prompt timed out while waiting for its turn")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prompt not answered")
	}
}

func TestRepoNameFromURL(t *testing.T) {
	ok := map[string]string{
		"https://github.com/o/repo.git":  "repo",
		"https://github.com/o/repo":      "repo",
		"https://github.com/o/repo/":     "repo",
		"https://github.com/o/repo.git/": "repo",
		"git@github.com:owner/repo.git":  "repo",
		"git@host:repo.git":              "repo",
		"/tmp/x/remote.git":              "remote",
		"ssh://git@h:22/o/r.name.git":    "r.name",
		`C:\src\proj.git`:                "proj",
	}
	for in, want := range ok {
		if got, err := repoNameFromURL(in); err != nil || got != want {
			t.Errorf("repoNameFromURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", ".", "..", "/", "https://h/o/..", "https://h/o/.git", "host:", "https://h/-x"} {
		if got, err := repoNameFromURL(in); err == nil {
			t.Errorf("repoNameFromURL(%q) = %q, want error", in, got)
		}
	}
}

// Shutdown lets a local write that is already running finish.
func TestShutdownLetsRunningCommitFinish(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.CommitAll("one")
	hooks := filepath.Join(repo.Dir, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "started")
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\ntouch '"+marker+"'\nsleep 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tab, err := a.OpenRepo(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	repo.Write("x.txt", "x\n")
	repo.Git("add", "x.txt")
	errc := make(chan error, 1)
	go func() {
		_, err := a.Commit(tab.ID, ops.CommitOptions{Message: "x"})
		errc <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pre-commit hook did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	a.shutdown()
	if err := <-errc; err != nil {
		t.Fatalf("commit running at shutdown: %v", err)
	}
	if c := repo.Git("rev-list", "--count", "HEAD"); c != "2" {
		t.Fatalf("commits = %s, the commit did not land", c)
	}
}

// When the tab cannot be saved, OpenRepo fails and leaves no orphan session.
func TestOpenRepoStoreFailureClosesSession(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.CommitAll("one")
	stateDir := filepath.Dir(a.cfg.StatePath)
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(stateDir, 0o700) })
	if _, err := a.OpenRepo(repo.Dir); err == nil {
		t.Fatal("OpenRepo succeeded although the state could not be saved")
	}
	a.mu.Lock()
	n := len(a.sessions)
	a.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d session(s) left open after the failed OpenRepo", n)
	}
}

func TestSettingsBindingsApplyAutoFetch(t *testing.T) {
	a, ev, repo := newTestApp(t)
	repo.CommitAll("one")
	set, err := a.GetSettings()
	if err != nil || !set.AutoFetch || set.AutoFetchSeconds != 60 {
		t.Fatalf("defaults = %+v %v", set, err)
	}
	ti, err := a.OpenRepo(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	// Enabled, but waiting for a first manual remote operation.
	st, err := a.AutoFetchStatus(ti.ID)
	if err != nil || !st.Enabled || st.IntervalSeconds != 60 || !st.Paused || st.Reason != session.PauseWaitingManual {
		t.Fatalf("status = %+v %v", st, err)
	}
	// The state is pushed to the frontend as autofetch:status.
	if d, ok := ev.find(session.EventAutoFetchStatus); !ok ||
		d != (session.AutoFetchEvent{Tab: ti.ID, Enabled: true, Paused: true, Reason: session.PauseWaitingManual}) {
		t.Fatalf("autofetch:status = %+v %v", d, ok)
	}
	set, err = a.SetSettings(store.Settings{AutoFetch: true, AutoFetchSeconds: 10})
	if err != nil || set.AutoFetchSeconds != store.MinAutoFetchSeconds {
		t.Fatalf("clamped = %+v %v", set, err)
	}
	if st, _ = a.AutoFetchStatus(ti.ID); st.IntervalSeconds != store.MinAutoFetchSeconds {
		t.Fatalf("not applied: %+v", st)
	}
	if _, err = a.SetSettings(store.Settings{AutoFetch: false, AutoFetchSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	if st, _ = a.AutoFetchStatus(ti.ID); st.Enabled {
		t.Fatalf("not disabled: %+v", st)
	}
	if set, _ = a.GetSettings(); set.AutoFetch || set.AutoFetchSeconds != 120 {
		t.Fatalf("not persisted: %+v", set)
	}
	// A tab opened later starts with the saved settings.
	other := testutil.NewRepoWith(t, repo.R)
	other.CommitAll("x")
	ti2, err := a.OpenRepo(other.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ = a.AutoFetchStatus(ti2.ID); st.Enabled {
		t.Fatalf("new tab ignores settings: %+v", st)
	}
	if _, err := a.AutoFetchStatus("nope"); decodeUIError(t, err).Kind != "NoTab" {
		t.Fatal("unknown tab must be NoTab")
	}
}

func TestRevealInFinder(t *testing.T) {
	a, _, repo := newTestApp(t)
	repo.Write("dir/a.txt", "a\n")
	repo.CommitAll("one")
	var revealed []string
	a.reveal = func(p string) error { revealed = append(revealed, p); return nil }
	ti, err := a.OpenRepo(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(repo.Dir)
	if err := a.RevealInFinder(ti.ID, "dir/a.txt"); err != nil {
		t.Fatal(err)
	}
	if len(revealed) != 1 || revealed[0] != filepath.Join(root, "dir", "a.txt") {
		t.Fatalf("revealed %v", revealed)
	}
	for _, p := range []string{"../outside", "/etc/passwd", ".git/config", ""} {
		if k := decodeUIError(t, a.RevealInFinder(ti.ID, p)).Kind; k != "InvalidFilePath" {
			t.Errorf("%q: kind %s", p, k)
		}
	}
	if len(revealed) != 1 {
		t.Fatalf("an invalid path reached the file manager: %v", revealed)
	}
	if decodeUIError(t, a.RevealInFinder("nope", "dir/a.txt")).Kind != "NoTab" {
		t.Fatal("unknown tab must be NoTab")
	}
}
