package askpass

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gitrais/internal/gitexec"
	"gitrais/internal/ops"
	"gitrais/internal/testutil"
)

const e2ePassword = "s3cret-pw-Zq81"

func buildHelper(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "askpass-helper")
	if b, err := exec.Command("go", "build", "-o", out, "gitrais/cmd/askpass-helper").CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, b)
	}
	return out
}

// e2eRunner returns an isolated runner that never consults a credential helper
// (in particular the macOS keychain): an empty credential.helper resets the list.
func e2eRunner(t *testing.T) (*gitexec.Runner, *bytes.Buffer) {
	t.Helper()
	r := testutil.Runner(t)
	r.Env = append(r.Env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
	)
	var logBuf bytes.Buffer
	r.Log = gitexec.NewActivityLog(&logBuf)
	return r, &logBuf
}

// httpRemote serves a bare repo (seeded with one commit) over HTTP smart protocol
// behind basic auth, and returns its URL.
func httpRemote(t *testing.T, r *gitexec.Runner, user, pass string) string {
	t.Helper()
	src := testutil.NewRepoWith(t, r)
	src.Write("a.txt", "hello\n")
	src.CommitAll("hello")
	bare := testutil.NewBare(t, r)
	src.Git("push", "-q", bare, "main")
	if _, err := r.Run(context.Background(), gitexec.Cmd{Dir: bare, Args: []string{"config", "http.receivepack", "true"}}); err != nil {
		t.Fatal(err)
	}
	execPath, err := r.Run(context.Background(), gitexec.Cmd{Args: []string{"--exec-path"}})
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{
		Path: filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend"),
		Root: "/",
		Env:  append([]string{"GIT_PROJECT_ROOT=" + filepath.Dir(bare), "GIT_HTTP_EXPORT_ALL=1"}, r.Env...),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		u, p, ok := req.BasicAuth()
		if !ok || u != user || p != pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/" + filepath.Base(bare)
}

func answerer(user, pass string, prompts *[]string, mu *sync.Mutex) Handler {
	return func(_ context.Context, p string) (string, bool) {
		mu.Lock()
		*prompts = append(*prompts, p)
		mu.Unlock()
		if strings.HasPrefix(p, "Username") {
			return user, true
		}
		return pass, true
	}
}

func TestE2ECloneAndPushWithAskPass(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	r, logBuf := e2eRunner(t)
	url := httpRemote(t, r, "alice", e2ePassword)
	var mu sync.Mutex
	var prompts []string
	s, err := Start(answerer("alice", e2ePassword, &prompts, &mu))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r.Env = append(r.Env, s.Env(buildHelper(t))...)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dest := filepath.Join(t.TempDir(), "c")
	if err := ops.Clone(ctx, r, url, dest, nil); err != nil {
		t.Fatalf("clone: %v", err)
	}
	mu.Lock()
	if len(prompts) != 2 || !strings.HasPrefix(prompts[0], "Username") || !strings.HasPrefix(prompts[1], "Password") {
		t.Fatalf("prompts = %v", prompts)
	}
	mu.Unlock()

	if _, err := r.Run(ctx, gitexec.Cmd{Dir: dest, Args: []string{"commit", "-q", "--allow-empty", "-m", "second"}}); err != nil {
		t.Fatal(err)
	}
	if err := ops.Push(ctx, r, dest, ops.PushOptions{Remote: "origin", Branch: "main"}, nil); err != nil {
		t.Fatalf("push: %v", err)
	}
	if strings.Contains(logBuf.String(), e2ePassword) {
		t.Fatal("password leaked into the activity log")
	}
}

func TestE2ECancelledPrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	r, logBuf := e2eRunner(t)
	url := httpRemote(t, r, "alice", e2ePassword)
	s, err := Start(func(context.Context, string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r.Env = append(r.Env, s.Env(buildHelper(t))...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dest := filepath.Join(t.TempDir(), "c")
	err = ops.Clone(ctx, r, url, dest, nil)
	if err == nil {
		t.Fatal("clone succeeded despite cancelled credential prompt")
	}
	if ctx.Err() != nil {
		t.Fatal("clone hung until the deadline")
	}
	if gitexec.KindOf(err) != gitexec.KindAuthFailed {
		t.Fatalf("want AuthFailed, got %v (%v)", gitexec.KindOf(err), err)
	}
	if strings.Contains(logBuf.String(), e2ePassword) {
		t.Fatal("password leaked into the activity log")
	}
}

func TestE2EWrongPasswordNoLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	r, logBuf := e2eRunner(t)
	url := httpRemote(t, r, "alice", e2ePassword)
	const wrong = "wrong-pw-Xk47"
	var mu sync.Mutex
	var prompts []string
	s, err := Start(answerer("alice", wrong, &prompts, &mu))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r.Env = append(r.Env, s.Env(buildHelper(t))...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = ops.Clone(ctx, r, url, filepath.Join(t.TempDir(), "c"), nil)
	if gitexec.KindOf(err) != gitexec.KindAuthFailed {
		t.Fatalf("want AuthFailed, got %v (%v)", gitexec.KindOf(err), err)
	}
	for _, secret := range []string{wrong, e2ePassword} {
		if strings.Contains(logBuf.String(), secret) || strings.Contains(err.Error(), secret) {
			t.Fatalf("secret %q leaked into log or error", secret)
		}
	}
	var ge *gitexec.GitError
	if asGitError(err, &ge) && (strings.Contains(ge.Stderr, wrong) || strings.Contains(strings.Join(ge.Args, " "), wrong)) {
		t.Fatal("password leaked into GitError")
	}
}

func asGitError(err error, target **gitexec.GitError) bool { return errors.As(err, target) }
