package ops

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

// FetchBackground must never prompt: with askpass helpers configured on the
// runner (as the app does) an HTTP 401 fails at once with AuthFailed and the
// helpers are never invoked.
func TestFetchBackgroundNeverPrompts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	marker := filepath.Join(t.TempDir(), "asked")
	helper := filepath.Join(t.TempDir(), "askpass.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ntouch '"+marker+"'\necho secret\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := testutil.Runner(t)
	r.Env = append(r.Env, "GIT_ASKPASS="+helper, "SSH_ASKPASS="+helper, "SSH_ASKPASS_REQUIRE=force")
	repo := testutil.NewRepoWith(t, r)
	repo.CommitAll("one")
	repo.Git("remote", "add", "origin", srv.URL+"/repo.git")

	// Sanity: a plain Fetch does use the helper.
	_ = Fetch(bg, r, repo.Dir, nil)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("plain fetch should have asked the helper: %v", err)
	}
	os.Remove(marker)

	err := FetchBackground(bg, r, repo.Dir)
	if gitexec.KindOf(err) != gitexec.KindAuthFailed {
		t.Fatalf("want AuthFailed, got %v", err)
	}
	if _, serr := os.Stat(marker); serr == nil {
		t.Fatal("the askpass helper was invoked by a background fetch")
	}
}

func TestFetchBackgroundFetches(t *testing.T) {
	a := testutil.NewRepo(t)
	a.CommitAll("one")
	bare := testutil.NewBare(t, a.R)
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")
	b := testutil.NewRepoWith(t, a.R)
	b.Git("remote", "add", "origin", bare)
	h := a.CommitAll("two")
	a.Git("push", "-q")
	if err := FetchBackground(bg, b.R, b.Dir); err != nil {
		t.Fatal(err)
	}
	if b.Git("rev-parse", "origin/main") != h {
		t.Fatal("origin/main not fetched")
	}
}

// BatchMode is forced on ssh only when the user has no ssh command of their own.
func TestBackgroundFetchArgsSSH(t *testing.T) {
	repo := testutil.NewRepo(t)
	t.Setenv("GIT_SSH_COMMAND", "")
	t.Setenv("GIT_SSH", "")
	os.Unsetenv("GIT_SSH_COMMAND")
	os.Unsetenv("GIT_SSH")
	args, err := backgroundFetchArgs(bg, repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, "core.sshCommand=ssh -o BatchMode=yes") {
		t.Fatalf("BatchMode not forced: %q", args)
	}
	repo.Git("config", "core.sshCommand", "my-ssh")
	if args, _ = backgroundFetchArgs(bg, repo.R, repo.Dir); slices.Contains(args, "-c") {
		t.Fatalf("user core.sshCommand overridden: %q", args)
	}
	repo.Git("config", "--unset", "core.sshCommand")
	t.Setenv("GIT_SSH", "/usr/bin/my-ssh")
	if args, _ = backgroundFetchArgs(bg, repo.R, repo.Dir); slices.Contains(args, "-c") {
		t.Fatalf("GIT_SSH overridden: %q", args)
	}
}
