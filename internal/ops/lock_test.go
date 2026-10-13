package ops

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitrais/internal/testutil"
)

// ageLock makes the lock look old enough to be considered stale.
func ageLock(t *testing.T, p string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveIndexLock(t *testing.T) {
	repo := testutil.NewRepo(t)
	gitDir := filepath.Join(repo.Dir, ".git")
	lock := filepath.Join(gitDir, "index.lock")

	if err := RemoveIndexLock(bg, gitDir, repo.Dir); err != nil {
		t.Fatalf("no lock: %v", err)
	}

	// A fresh lock may belong to a git command that is just starting.
	os.WriteFile(lock, nil, 0o644)
	if err := RemoveIndexLock(bg, gitDir, repo.Dir); !errors.Is(err, ErrLockTooRecent) {
		t.Fatalf("fresh lock: want ErrLockTooRecent, got %v", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("fresh lock must be left alone")
	}

	// Held open by this process: refused even when old.
	ageLock(t, lock)
	f, err := os.Open(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := RemoveIndexLock(bg, gitDir, repo.Dir); !errors.Is(err, ErrLockInUse) {
		t.Fatalf("open lock: want ErrLockInUse, got %v", err)
	}
	f.Close()

	// Stale and unheld: removed.
	if err := RemoveIndexLock(bg, gitDir, repo.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("lock not removed")
	}
}

func TestRemoveIndexLockHeldByChild(t *testing.T) {
	repo := testutil.NewRepo(t)
	gitDir := filepath.Join(repo.Dir, ".git")
	lock := filepath.Join(gitDir, "index.lock")
	os.WriteFile(lock, nil, 0o644)
	ageLock(t, lock)
	cmd := exec.Command("/bin/sh", "-c", "exec 3>"+lock+"; sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := RemoveIndexLock(bg, gitDir, repo.Dir)
		if errors.Is(err, ErrLockInUse) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock held by a child: want ErrLockInUse, got %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("held lock must not be removed")
	}
}

func TestRemoveIndexLockLsofMissing(t *testing.T) {
	repo := testutil.NewRepo(t)
	gitDir := filepath.Join(repo.Dir, ".git")
	lock := filepath.Join(gitDir, "index.lock")
	os.WriteFile(lock, nil, 0o644)
	ageLock(t, lock)
	old := lsofPath
	lsofPath = filepath.Join(t.TempDir(), "no-lsof")
	t.Cleanup(func() { lsofPath = old })
	if err := RemoveIndexLock(bg, gitDir, repo.Dir); !errors.Is(err, ErrLsofMissing) {
		t.Fatalf("want ErrLsofMissing, got %v", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("lock must not be deleted blindly")
	}
}

func TestRemoveIndexLockOnlyInsideGitDir(t *testing.T) {
	if err := RemoveIndexLock(bg, "", ""); err == nil {
		t.Fatal("empty gitDir must be refused")
	}
	if err := RemoveIndexLock(bg, "relative/.git", "relative"); err == nil {
		t.Fatal("relative gitDir must be refused")
	}
	// A symlinked lock is never followed or removed.
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "victim")
	os.WriteFile(target, []byte("x"), 0o644)
	if err := os.Symlink(target, filepath.Join(dir, "index.lock")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIndexLock(bg, dir, dir); err == nil {
		t.Fatal("symlink lock must be refused")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("symlink target touched")
	}
}

func TestRemoveIndexLockLsofTimeoutFailsClosed(t *testing.T) {
	repo := testutil.NewRepo(t)
	gitDir := filepath.Join(repo.Dir, ".git")
	lock := filepath.Join(gitDir, "index.lock")
	os.WriteFile(lock, nil, 0o644)
	ageLock(t, lock)
	fake := filepath.Join(t.TempDir(), "lsof")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath, oldTO := lsofPath, lsofTimeout
	lsofPath, lsofTimeout = fake, 200*time.Millisecond
	t.Cleanup(func() { lsofPath, lsofTimeout = oldPath, oldTO })
	start := time.Now()
	err := RemoveIndexLock(bg, gitDir, repo.Dir)
	if err == nil {
		t.Fatal("a hung lsof must make the removal fail")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("lsof was not bounded: %v", time.Since(start))
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("lock must be kept when lsof times out")
	}
}

// fakeLsof installs an lsof that reports no holder of the lock and prints
// cwdOut for the git-cwd query (or fails with exit status cwdExit).
func fakeLsof(t *testing.T, cwdOut string, cwdExit int) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "cwd.out")
	if err := os.WriteFile(out, []byte(cwdOut), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"case \"$*\" in *cwd*) cat '" + out + "'; exit " + strconv.Itoa(cwdExit) + ";; esac\n" +
		"exit 1\n"
	fake := filepath.Join(dir, "lsof")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := lsofPath
	lsofPath = fake
	t.Cleanup(func() { lsofPath = old })
}

// A git process working inside the repository (worktree or git dir) may be
// about to take the lock again: removal is refused.
func TestRemoveIndexLockGitRunningInRepo(t *testing.T) {
	repo := testutil.NewRepo(t)
	gitDir := filepath.Join(repo.Dir, ".git")
	lock := filepath.Join(gitDir, "index.lock")
	other := t.TempDir()
	cases := []struct {
		name, out string
		exit      int
		want      error
	}{
		{"cwd in worktree", "p123\nfcwd\nn" + filepath.Join(repo.Dir, "sub") + "\n", 0, ErrGitRunning},
		{"cwd is git dir", "p123\nfcwd\nn" + gitDir + "\n", 0, ErrGitRunning},
		{"cwd differs in case", "p123\nfcwd\nn" + strings.ToUpper(repo.Dir) + "\n", 0, ErrGitRunning},
		{"lsof fails", "", 2, errLsofFailed},
		{"git elsewhere", "p123\nfcwd\nn" + other + "\np124\nfcwd\nn" + repo.Dir + "-other\n", 0, nil},
		{"no git process", "", 1, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "cwd differs in case" && runtime.GOOS != "darwin" {
				t.Skip("case-insensitive paths only on darwin")
			}
			os.WriteFile(lock, nil, 0o644)
			ageLock(t, lock)
			fakeLsof(t, c.out, c.exit)
			err := RemoveIndexLock(bg, gitDir, repo.Dir)
			_, statErr := os.Stat(lock)
			if c.want == nil {
				if err != nil || statErr == nil {
					t.Fatalf("err = %v, lock removed = %v", err, statErr != nil)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if statErr != nil {
				t.Fatal("lock removed despite the refusal")
			}
		})
	}
}
