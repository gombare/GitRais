package watch

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"gitrais/internal/testutil"
)

// eventually polls cond for up to 3s.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// newTestFilter returns a filter with short timings; it is closed at test end.
func newTestFilter(t *testing.T, repo *testutil.Repo, minInterval time.Duration) *IgnoreFilter {
	t.Helper()
	f, err := NewIgnoreFilter(t.Context(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	f.delay = 20 * time.Millisecond
	f.minInterval = minInterval
	t.Cleanup(func() { f.Close() })
	return f
}

func TestIgnoredPaths(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "build/\n*.log\n")
	repo.Write("build/out.bin", "x")
	repo.Write("a.log", "x")
	repo.Write("src/main.go", "x")
	ign, err := IgnoredPaths(t.Context(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ign("build/out.bin") || !ign("build") || !ign("a.log") || ign("src/main.go") {
		t.Fatal("wrong ignore predicate")
	}
	if !ign("build/") || !ign("build/new/deeper/file") {
		t.Error("paths below an ignored directory must be ignored")
	}
	if ign("builder") || ign("buildx/out.bin") || ign("src") {
		t.Error("prefix of a name must not match")
	}
}

func TestIgnoredPathsRefreshOnGitignoreChange(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "*.log\n")
	repo.Write("a.log", "x")
	repo.Write("b.tmp", "x")
	ign, err := IgnoredPaths(t.Context(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if ign("b.tmp") {
		t.Fatal("b.tmp must not be ignored yet")
	}
	repo.Write(".gitignore", "*.log\n*.tmp\n")
	if ign(".gitignore") {
		t.Error(".gitignore itself must not be ignored")
	}
	eventually(t, "snapshot refresh after a .gitignore change", func() bool { return ign("b.tmp") && ign("a.log") })
}

func TestIgnoredPathsKeepsSnapshotOnFailure(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "*.log\n")
	repo.Write("a.log", "x")
	f := newTestFilter(t, repo, 0)
	var failed atomic.Int32
	f.onRebuild = func(err error) {
		if err != nil {
			failed.Add(1)
		}
	}
	if err := os.RemoveAll(repo.Path(".git")); err != nil {
		t.Fatal(err)
	}
	f.Ignored(".gitignore") // refresh fails: git no longer finds a repository
	eventually(t, "the failed rebuild", func() bool { return failed.Load() == 1 })
	if !f.Ignored("a.log") {
		t.Error("previous snapshot must survive a failed refresh")
	}
}

func TestIgnoreFilterOneRebuildPerBurst(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "*.log\n")
	repo.Write("a.log", "x")
	f := newTestFilter(t, repo, 0)
	start := time.Now()
	for i := 0; i < 20; i++ {
		f.Ignored(fmt.Sprintf("pkg%d/.gitignore", i%5)) // nested .gitignore files...
		f.Ignored(".gitignore")
	}
	f.Ignored(excludeRel) // ...and info/exclude, all in one batch
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("Ignored ran the rebuild on the caller's goroutine (%v)", d)
	}
	if n := f.rebuilds.Load(); n != 0 {
		t.Fatalf("rebuild must be deferred, got %d", n)
	}
	eventually(t, "one rebuild", func() bool { return f.rebuilds.Load() == 1 })
	time.Sleep(100 * time.Millisecond)
	if n := f.rebuilds.Load(); n != 1 {
		t.Fatalf("rebuilds = %d, want exactly 1", n)
	}
	if !f.Ignored("a.log") {
		t.Error("snapshot lost across rebuild")
	}
}

func TestIgnoreFilterInfoExclude(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("secret.txt", "x")
	f := newTestFilter(t, repo, 0)
	if f.Ignored("secret.txt") {
		t.Fatal("not ignored yet")
	}
	repo.Write(".git/info/exclude", "secret.txt\n")
	f.Ignored(excludeRel)
	eventually(t, "info/exclude to apply", func() bool { return f.Ignored("secret.txt") })
}

func TestIgnoreFilterNewIgnoredDirectory(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "node_modules/\n")
	repo.Write("src/main.go", "x")
	f := newTestFilter(t, repo, 2*time.Second)
	// npm install after the snapshot.
	for i := 0; i < 100; i++ {
		repo.Write(fmt.Sprintf("node_modules/p%d/i.js", i), "x")
	}
	for i := 0; i < 100; i++ {
		f.Ignored(fmt.Sprintf("node_modules/p%d/i.js", i))
	}
	eventually(t, "node_modules to be ignored", func() bool { return f.Ignored("node_modules/p/i.js") })
	if n := f.rebuilds.Load(); n != 1 {
		t.Fatalf("rebuilds = %d, want 1 for the whole burst", n)
	}
	// The directory is known now: more paths in it do not trigger rebuilds.
	for i := 0; i < 100; i++ {
		f.Ignored(fmt.Sprintf("node_modules/q%d/i.js", i))
	}
	f.Ignored("src/main.go")
	time.Sleep(150 * time.Millisecond)
	if n := f.rebuilds.Load(); n != 1 {
		t.Fatalf("rebuilds = %d after the directory became known, want 1", n)
	}
}

func TestIgnoreFilterRateLimit(t *testing.T) {
	repo := testutil.NewRepo(t)
	f := newTestFilter(t, repo, 400*time.Millisecond)
	f.Ignored(".gitignore")
	eventually(t, "first rebuild", func() bool { return f.rebuilds.Load() == 1 })
	f.Ignored("fresh1.txt") // unknown top-level names
	f.Ignored("fresh2.txt")
	time.Sleep(150 * time.Millisecond)
	if n := f.rebuilds.Load(); n != 1 {
		t.Fatalf("second rebuild ran before the minimum interval (%d)", n)
	}
	eventually(t, "second rebuild", func() bool { return f.rebuilds.Load() == 2 })
}

func TestIgnoredPathsNewIgnoredDirectory(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "build/\n")
	repo.Write("src/main.go", "x")
	ign, err := IgnoredPaths(t.Context(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	repo.Write("build/out.bin", "x")
	ign("build/out.bin")
	eventually(t, "build/ to be ignored", func() bool { return ign("build/out.bin") })
}

func TestIgnoreFilterCloseStopsRebuilds(t *testing.T) {
	repo := testutil.NewRepo(t)
	f := newTestFilter(t, repo, 0)
	f.Ignored(".gitignore")
	f.Close()
	f.Close()
	time.Sleep(80 * time.Millisecond)
	f.Ignored(".gitignore")
	time.Sleep(80 * time.Millisecond)
	if n := f.rebuilds.Load(); n != 0 {
		t.Fatalf("rebuild after Close: %d", n)
	}
}

func TestIgnoredPathsError(t *testing.T) {
	r := testutil.Runner(t)
	if _, err := IgnoredPaths(t.Context(), r, t.TempDir()); err == nil {
		t.Fatal("expected an error outside a repository")
	}
}
