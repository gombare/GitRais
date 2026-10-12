// Package testutil creates isolated temporary git repositories for tests.
package testutil

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitrais/internal/gitexec"
)

// IsolatedEnv makes tests independent of the user's and system's git configuration.
func IsolatedEnv(home string) []string {
	return []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	}
}

func Runner(t testing.TB) *gitexec.Runner {
	t.Helper()
	git, err := gitexec.Locate()
	if err != nil {
		t.Fatalf("bundled git not found (run `make git`): %v", err)
	}
	return &gitexec.Runner{Git: git, Env: IsolatedEnv(realTemp(t))}
}

type Repo struct {
	t   testing.TB
	Dir string
	R   *gitexec.Runner
}

func NewRepo(t testing.TB) *Repo { return NewRepoWith(t, Runner(t)) }

func NewRepoWith(t testing.TB, r *gitexec.Runner) *Repo {
	t.Helper()
	repo := &Repo{t: t, Dir: realTemp(t), R: r}
	repo.Git("init", "-q", "-b", "main")
	return repo
}

// NewBare creates a bare repository to use as a local remote.
func NewBare(t testing.TB, r *gitexec.Runner) string {
	t.Helper()
	dir := filepath.Join(realTemp(t), "remote.git")
	if _, err := r.Run(context.Background(), gitexec.Cmd{Args: []string{"init", "-q", "--bare", "-b", "main", dir}}); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	return dir
}

func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	out, err := r.R.Run(context.Background(), gitexec.Cmd{Dir: r.Dir, Args: args})
	if err != nil {
		r.t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func (r *Repo) Path(rel string) string { return filepath.Join(r.Dir, rel) }

func (r *Repo) Write(rel, content string) {
	r.t.Helper()
	p := r.Path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *Repo) CommitAll(msg string) string {
	r.t.Helper()
	r.Git("add", "-A")
	r.Git("commit", "-q", "--allow-empty", "-m", msg)
	return r.Git("rev-parse", "HEAD")
}

// realTemp returns a temporary directory without symlinks (/var → /private/var),
// needed because FSEvents reports real paths.
func realTemp(t testing.TB) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}
