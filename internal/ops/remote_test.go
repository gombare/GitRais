package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

func TestCloneFetchPullPush(t *testing.T) {
	a := testutil.NewRepo(t)
	a.Write("a.txt", "1\n")
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")

	dest := filepath.Join(t.TempDir(), "clone")
	var phases []string
	if err := Clone(bg, a.R, bare, dest, func(p gitexec.Progress) { phases = append(phases, p.Phase) }); err != nil {
		t.Fatal(err)
	}
	a.Write("a.txt", "2\n")
	a.CommitAll("second")
	a.Git("push", "-q")
	if err := Fetch(bg, a.R, dest, nil); err != nil {
		t.Fatal(err)
	}
	if err := Pull(bg, a.R, dest, PullFFOnly, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := a.R.Run(bg, gitexec.Cmd{Dir: dest, Args: []string{"log", "-1", "--format=%s"}})
	if string(out) != "second\n" {
		t.Fatalf("pull: HEAD = %q", out)
	}

	cl := testutil.NewRepoWith(t, a.R)
	cl.Git("remote", "add", "origin", bare)
	cl.Git("fetch", "-q", "origin")
	cl.Git("switch", "-q", "-c", "topic", "origin/main")
	cl.Write("t.txt", "t\n")
	cl.CommitAll("topic work")
	if err := Push(bg, cl.R, cl.Dir, PushOptions{Remote: "origin", Branch: "topic", SetUpstream: true}, nil); err != nil {
		t.Fatal(err)
	}
	if cl.Git("rev-parse", "--abbrev-ref", "topic@{upstream}") != "origin/topic" {
		t.Fatal("upstream not set")
	}
}

func TestPushNonFastForward(t *testing.T) {
	a := testutil.NewRepo(t)
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")
	b := testutil.NewRepoWith(t, a.R)
	b.Git("remote", "add", "origin", bare)
	b.Git("fetch", "-q", "origin")
	b.Git("switch", "-q", "-C", "main", "origin/main")
	b.CommitAll("b")
	b.Git("push", "-q", "origin", "main")
	a.CommitAll("a")
	err := Push(bg, a.R, a.Dir, PushOptions{Remote: "origin", Branch: "main"}, nil)
	if gitexec.KindOf(err) != gitexec.KindNonFastForward {
		t.Fatalf("want NonFastForward, got %v", err)
	}
	// Stale lease: a has not fetched b's commit, so the lease must reject the push.
	if err := Push(bg, a.R, a.Dir, PushOptions{Remote: "origin", Branch: "main", ForceWithLease: true}, nil); err == nil {
		t.Fatal("force-with-lease with a stale tracking ref must fail")
	}
	bHead := b.Git("rev-parse", "HEAD")
	if got := a.Git("--git-dir="+bare, "rev-parse", "main"); got != bHead {
		t.Fatalf("remote was overwritten: %s != %s", got, bHead)
	}
	a.Git("fetch", "-q", "origin") // lease compares with the local origin/main: must be current
	if err := Push(bg, a.R, a.Dir, PushOptions{Remote: "origin", Branch: "main", ForceWithLease: true}, nil); err != nil {
		t.Fatalf("force-with-lease: %v", err)
	}
	if got := a.Git("--git-dir="+bare, "rev-parse", "main"); got != a.Git("rev-parse", "HEAD") {
		t.Fatal("remote not updated by force-with-lease")
	}
}

func TestPushRemoteBranchAndForceNewBranch(t *testing.T) {
	a := testutil.NewRepo(t)
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	// Lease on a branch absent from the remote (no tracking ref): allowed, expects absence.
	if err := Push(bg, a.R, a.Dir, PushOptions{Remote: "origin", Branch: "main", RemoteBranch: "other", SetUpstream: true, ForceWithLease: true}, nil); err != nil {
		t.Fatal(err)
	}
	if a.Git("rev-parse", "--abbrev-ref", "main@{upstream}") != "origin/other" {
		t.Fatal("upstream should be origin/other")
	}
	// Second lease push on the now existing branch where the tracking ref is current.
	a.CommitAll("more")
	if err := Push(bg, a.R, a.Dir, PushOptions{Remote: "origin", Branch: "main", RemoteBranch: "other", ForceWithLease: true}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPushRejectsBadArgs(t *testing.T) {
	a := testutil.NewRepo(t)
	a.CommitAll("base")
	for _, o := range []PushOptions{
		{Remote: "--receive-pack=x", Branch: "main"},
		{Remote: "origin", Branch: "--delete"},
		{Remote: "origin", Branch: "main", RemoteBranch: "-f"},
		{Remote: "origin", Branch: "bad..name"},
		{Remote: "", Branch: "main"},
		{Remote: "origin", Branch: "", ForceWithLease: true},
	} {
		if err := Push(bg, a.R, a.Dir, o, nil); err == nil || gitexec.KindOf(err) != gitexec.KindUnknown {
			t.Errorf("%+v: want validation error, got %v", o, err)
		} else if _, ok := err.(*gitexec.GitError); ok {
			t.Errorf("%+v: git was run: %v", o, err)
		}
	}
}

// divergedClone returns a clone whose main has one local commit while the
// remote has another (conflicting when conflict is true).
func divergedClone(t *testing.T, conflict bool) (dest string, r *gitexec.Runner) {
	t.Helper()
	a := testutil.NewRepo(t)
	a.Write("f.txt", "base\n")
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")
	dest = filepath.Join(t.TempDir(), "clone")
	if err := Clone(bg, a.R, bare, dest, nil); err != nil {
		t.Fatal(err)
	}
	a.Write("f.txt", "remote\n")
	if !conflict {
		a.Write("f.txt", "base\n")
		a.Write("r.txt", "remote\n")
	}
	a.CommitAll("remote change")
	a.Git("push", "-q")
	file := "l.txt"
	if conflict {
		file = "f.txt"
	}
	if err := os.WriteFile(filepath.Join(dest, file), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, a.R, dest, "add", "-A")
	gitIn(t, a.R, dest, "commit", "-q", "-m", "local change")
	return dest, a.R
}

func gitIn(t *testing.T, r *gitexec.Runner, dir string, args ...string) string {
	t.Helper()
	out, err := r.Run(bg, gitexec.Cmd{Dir: dir, Args: args})
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestPullDefaultDivergedDoesNotPrompt(t *testing.T) {
	dest, r := divergedClone(t, false)
	err := Pull(bg, r, dest, PullDefault, nil)
	if !errors.Is(err, ErrPullStrategyRequired) {
		t.Fatalf("want ErrPullStrategyRequired, got %v", err)
	}
}

func TestPullMergeDivergedCreatesMergeCommitWithoutEditor(t *testing.T) {
	dest, r := divergedClone(t, false)
	if err := Pull(bg, r, dest, PullMerge, nil); err != nil {
		t.Fatal(err)
	}
	if n := gitIn(t, r, dest, "rev-list", "--parents", "-1", "HEAD"); len(strings.Fields(n)) != 3 {
		t.Fatalf("HEAD is not a merge commit: %q", n)
	}
}

func TestPullRebaseDiverged(t *testing.T) {
	dest, r := divergedClone(t, false)
	if err := Pull(bg, r, dest, PullRebase, nil); err != nil {
		t.Fatal(err)
	}
	if gitIn(t, r, dest, "log", "-1", "--format=%s") != "local change" {
		t.Fatal("local commit should be on top after rebase")
	}
}

func TestPullFFOnlyDivergedFails(t *testing.T) {
	dest, r := divergedClone(t, false)
	if err := Pull(bg, r, dest, PullFFOnly, nil); err == nil {
		t.Fatal("ff-only on diverged branches must fail")
	}
}

func TestPullConflictKind(t *testing.T) {
	dest, r := divergedClone(t, true)
	err := Pull(bg, r, dest, PullMerge, nil)
	if gitexec.KindOf(err) != gitexec.KindConflict {
		t.Fatalf("merge: want Conflict, got %v", err)
	}
	gitIn(t, r, dest, "merge", "--abort")

	dest, r = divergedClone(t, true)
	err = Pull(bg, r, dest, PullRebase, nil)
	if gitexec.KindOf(err) != gitexec.KindConflict {
		t.Fatalf("rebase: want Conflict, got %v", err)
	}
	// The stopped rebase is left in place for the UI to continue/abort.
	if _, serr := os.Stat(filepath.Join(dest, ".git", "rebase-merge")); serr != nil {
		t.Fatalf("rebase state should remain: %v", serr)
	}
}

func TestCloneProgress(t *testing.T) {
	a := testutil.NewRepo(t)
	a.Write("a.txt", "1\n")
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")
	var got []gitexec.Progress
	dest := filepath.Join(t.TempDir(), "c")
	if err := Clone(bg, a.R, "file://"+bare, dest, func(p gitexec.Progress) { got = append(got, p) }); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no progress reported")
	}
}

func TestCloneDestinations(t *testing.T) {
	a := testutil.NewRepo(t)
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")

	busy := t.TempDir()
	if err := os.WriteFile(filepath.Join(busy, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Clone(bg, a.R, bare, busy, nil); !errors.Is(err, ErrDestNotEmpty) {
		t.Fatalf("want ErrDestNotEmpty, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(busy, "x")); err != nil {
		t.Fatal("existing content must be untouched")
	}
	empty := t.TempDir()
	if err := Clone(bg, a.R, bare, empty, nil); err != nil {
		t.Fatalf("empty existing dir should work: %v", err)
	}
	for _, bad := range []string{"-x", ""} {
		if err := Clone(bg, a.R, bad, filepath.Join(t.TempDir(), "d"), nil); err == nil {
			t.Errorf("url %q should be rejected", bad)
		}
	}
	if err := Clone(bg, a.R, bare, "", nil); err == nil {
		t.Error("empty dest should be rejected")
	}
}

func TestCloneFailureCleansUpAndRedactsCredentials(t *testing.T) {
	r := testutil.Runner(t)
	parent := t.TempDir()
	dest := filepath.Join(parent, "sub", "dest")
	url := "http://user:s3cretpw@127.0.0.1:1/repo.git"
	err := Clone(bg, r, url, dest, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	var ge *gitexec.GitError
	if !errors.As(err, &ge) {
		t.Fatalf("want GitError, got %T", err)
	}
	if strings.Contains(err.Error(), "s3cretpw") || strings.Contains(ge.Stderr, "s3cretpw") || strings.Contains(strings.Join(ge.Args, " "), "s3cretpw") {
		t.Fatalf("credentials leaked: %v / %q / %v", err, ge.Stderr, ge.Args)
	}
	if _, serr := os.Stat(dest); !os.IsNotExist(serr) {
		t.Fatalf("half-created dest left behind: %v", serr)
	}
}

func TestInit(t *testing.T) {
	r := testutil.Runner(t)
	dir := filepath.Join(t.TempDir(), "new")
	if err := Init(bg, r, dir); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(bg, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "--is-inside-work-tree"}})
	if err != nil || string(out) != "true\n" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCloneDanglingSymlinkDestIsNotDeleted(t *testing.T) {
	a := testutil.NewRepo(t)
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")
	dir := t.TempDir()
	dest := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "nowhere"), dest); err != nil {
		t.Fatal(err)
	}
	if err := Clone(bg, a.R, bare, dest, nil); err == nil {
		t.Fatal("symlink dest must be rejected")
	}
	if _, err := os.Lstat(dest); err != nil {
		t.Fatalf("symlink must still exist: %v", err)
	}
	// A failing clone must not delete it either.
	if err := Clone(bg, a.R, "http://127.0.0.1:1/repo.git", dest, nil); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Lstat(dest); err != nil {
		t.Fatalf("symlink must still exist after failed clone: %v", err)
	}
}

func TestCloneFailureKeepsPreexistingEmptyDir(t *testing.T) {
	r := testutil.Runner(t)
	dest := t.TempDir()
	if err := Clone(bg, r, "http://127.0.0.1:1/repo.git", dest, nil); err == nil {
		t.Fatal("expected failure")
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("dir must still exist: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("dir must be empty, has %d entries", len(entries))
	}
}

func TestCloneInitRejectRelativePaths(t *testing.T) {
	r := testutil.Runner(t)
	if err := Clone(bg, r, "http://127.0.0.1:1/repo.git", "rel/dest", nil); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("Clone relative dest: %v", err)
	}
	if err := Init(bg, r, "rel/dir"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("Init relative dir: %v", err)
	}
	if _, err := os.Stat("rel"); err == nil {
		t.Fatal("relative dir must not be created")
	}
}

func TestPushHookRejectedErrorMessageIsMeaningful(t *testing.T) {
	a := testutil.NewRepo(t)
	a.Write("a.txt", "1\n")
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")
	hook := filepath.Join(bare, "hooks", "pre-receive")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'no pushes today'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.Write("a.txt", "2\n")
	a.CommitAll("two")
	err := Push(bg, a.R, a.Dir, PushOptions{Remote: "origin", Branch: "main"}, nil)
	if err == nil {
		t.Fatal("expected rejection")
	}
	if gitexec.KindOf(err) != gitexec.KindHookRejected {
		t.Fatalf("want HookRejected, got %v", err)
	}
	if strings.Contains(err.Error(), "Enumerating objects") || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("unhelpful message: %q", err.Error())
	}
}

// dirtyBehindClone returns a clone whose main is one commit behind its
// upstream, with f.txt modified (uncommitted) where the incoming commit also
// changes it. With conflict the two edits touch the same line; otherwise they
// touch distant lines and re-applying the local change merges cleanly.
func dirtyBehindClone(t *testing.T, conflict bool) (dest string, r *gitexec.Runner) {
	t.Helper()
	a := testutil.NewRepo(t)
	a.Write("f.txt", "1\n2\n3\n4\n5\n6\n7\n8\n")
	a.CommitAll("base")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")
	dest = filepath.Join(t.TempDir(), "clone")
	if err := Clone(bg, a.R, bare, dest, nil); err != nil {
		t.Fatal(err)
	}
	a.Write("f.txt", "remote\n2\n3\n4\n5\n6\n7\n8\n")
	a.CommitAll("remote change")
	a.Git("push", "-q")
	local := "1\n2\n3\n4\n5\n6\n7\nlocal\n"
	if conflict {
		local = "local\n2\n3\n4\n5\n6\n7\n8\n"
	}
	if err := os.WriteFile(filepath.Join(dest, "f.txt"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	return dest, a.R
}

func stashCount(t *testing.T, r *gitexec.Runner, dir string) int {
	t.Helper()
	out := gitIn(t, r, dir, "stash", "list")
	if out == "" {
		return 0
	}
	return len(strings.Split(out, "\n"))
}

func TestPullDirtyOverlappingFailsWithoutAutostash(t *testing.T) {
	// Rebase refuses up front ("cannot pull with rebase: You have unstaged changes").
	for _, mode := range []PullMode{PullDefault, PullRebase} {
		dest, r := dirtyBehindClone(t, false)
		err := Pull(bg, r, dest, mode, nil)
		if gitexec.KindOf(err) != gitexec.KindDirtyWorktree {
			t.Fatalf("%q: want DirtyWorktree, got %v", mode, err)
		}
	}
}

// An untracked file where the pull brings a tracked one is UntrackedOverwrite,
// not DirtyWorktree (autostash would not help).
func TestPullUntrackedInTheWay(t *testing.T) {
	for _, mode := range []PullMode{PullDefault, PullRebase} {
		dest, r := dirtyBehindClone(t, false)
		gitIn(t, r, dest, "checkout", "--", "f.txt")
		bare := gitIn(t, r, dest, "remote", "get-url", "origin")
		a := testutil.NewRepoWith(t, r)
		a.Git("pull", "-q", bare, "main")
		a.Write("new.txt", "remote\n")
		a.CommitAll("remote adds new.txt")
		a.Git("push", "-q", bare, "HEAD:main")
		if err := os.WriteFile(filepath.Join(dest, "new.txt"), []byte("mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := Pull(bg, r, dest, mode, nil)
		if gitexec.KindOf(err) != gitexec.KindUntrackedOverwrite {
			t.Fatalf("%q: want UntrackedOverwrite, got %v", mode, err)
		}
	}
}

func TestPullAutostashKeepsLocalChanges(t *testing.T) {
	for _, mode := range []PullMode{PullDefault, PullMerge, PullRebase, PullFFOnly} {
		t.Run(string(mode), func(t *testing.T) {
			dest, r := dirtyBehindClone(t, false)
			if err := PullAutostash(bg, r, dest, mode, nil); err != nil {
				t.Fatal(err)
			}
			if gitIn(t, r, dest, "log", "-1", "--format=%s") != "remote change" {
				t.Fatal("pull did not integrate the remote commit")
			}
			b, _ := os.ReadFile(filepath.Join(dest, "f.txt"))
			if string(b) != "remote\n2\n3\n4\n5\n6\n7\nlocal\n" {
				t.Fatalf("local change lost: %q", b)
			}
			if n := stashCount(t, r, dest); n != 0 {
				t.Fatalf("autostash left %d stash entries", n)
			}
		})
	}
}

func TestPullAutostashConflictKeepsStash(t *testing.T) {
	for _, mode := range []PullMode{PullDefault, PullRebase} {
		t.Run(string(mode), func(t *testing.T) {
			dest, r := dirtyBehindClone(t, true)
			err := PullAutostash(bg, r, dest, mode, nil)
			if !errors.Is(err, ErrAutostashConflict) {
				t.Fatalf("want ErrAutostashConflict, got %v", err)
			}
			if n := stashCount(t, r, dest); n != 1 {
				t.Fatalf("the stash must be kept, have %d entries", n)
			}
			if gitIn(t, r, dest, "log", "-1", "--format=%s") != "remote change" {
				t.Fatal("pull did not integrate the remote commit")
			}
			if gitIn(t, r, dest, "ls-files", "-u") == "" {
				t.Fatal("conflict markers expected in the index")
			}
		})
	}
}
