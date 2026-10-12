package askpass

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAskRoundTrip(t *testing.T) {
	var got string
	s, err := Start(func(_ context.Context, prompt string) (string, bool) {
		got = prompt
		return "alice", true
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ans, err := Ask(s.sock, s.token, "Username for 'https://h': ")
	if err != nil || ans != "alice" || got != "Username for 'https://h': " {
		t.Fatalf("ans=%q err=%v prompt=%q", ans, err, got)
	}
}

func TestAskRejectsBadToken(t *testing.T) {
	called := false
	s, _ := Start(func(context.Context, string) (string, bool) { called = true; return "x", true })
	defer s.Close()
	if _, err := Ask(s.sock, "wrong", "p"); err == nil {
		t.Fatal("wrong token accepted")
	}
	if called {
		t.Fatal("handler invoked with a wrong token")
	}
}

func TestAskCancelled(t *testing.T) {
	s, _ := Start(func(context.Context, string) (string, bool) { return "", false })
	defer s.Close()
	if _, err := Ask(s.sock, s.token, "p"); err == nil {
		t.Fatal("cancellation not propagated")
	}
}

func TestEnv(t *testing.T) {
	s, _ := Start(func(context.Context, string) (string, bool) { return "", false })
	defer s.Close()
	env := strings.Join(s.Env("/h"), "\n")
	for _, want := range []string{"GIT_ASKPASS=/h", "SSH_ASKPASS=/h", "SSH_ASKPASS_REQUIRE=force", "GIT_TERMINAL_PROMPT=0", EnvSock + "=", EnvToken + "="} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %s in %s", want, env)
		}
	}
}

func TestPermissions(t *testing.T) {
	s, _ := Start(func(context.Context, string) (string, bool) { return "", false })
	defer s.Close()
	di, err := os.Stat(filepath.Dir(s.sock))
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v err=%v", di.Mode(), err)
	}
	si, err := os.Stat(s.sock)
	if err != nil || si.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v err=%v", si.Mode(), err)
	}
	if len(s.token) != 64 {
		t.Fatalf("token length = %d", len(s.token))
	}
}

func TestOversizedRequestRejected(t *testing.T) {
	called := false
	s, _ := Start(func(context.Context, string) (string, bool) { called = true; return "x", true })
	defer s.Close()
	if _, err := Ask(s.sock, s.token, strings.Repeat("a", maxRequest+10)); err == nil {
		t.Fatal("oversized request accepted")
	}
	if called {
		t.Fatal("handler invoked for an oversized request")
	}
}

func TestStuckClientDoesNotHangClose(t *testing.T) {
	s, _ := Start(func(context.Context, string) (string, bool) { return "x", true })
	c, err := net.Dial("unix", s.sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close() // connects, never sends anything
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close hangs on a stuck client")
	}
}

func TestCloseUnblocksHandlerAndRemovesDir(t *testing.T) {
	entered := make(chan struct{})
	s, _ := Start(func(ctx context.Context, _ string) (string, bool) {
		close(entered)
		<-ctx.Done()
		return "", false
	})
	errc := make(chan error, 1)
	go func() { _, err := Ask(s.sock, s.token, "p"); errc <- err }()
	<-entered
	dir := filepath.Dir(s.sock)
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not unblock the pending handler")
	}
	if err := <-errc; err == nil {
		t.Fatal("pending Ask should fail after Close")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("socket dir still present: %v", err)
	}
}

func TestCloseUnblocksIgnoringHandler(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	entered := make(chan struct{})
	s, _ := Start(func(context.Context, string) (string, bool) { close(entered); <-block; return "", false })
	go Ask(s.sock, s.token, "p")
	<-entered
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close hangs on a handler that ignores ctx")
	}
}

func TestStartLongTmpdir(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("d", 60), strings.Repeat("e", 60))
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", long)
	s, err := Start(func(context.Context, string) (string, bool) { return "ok", true })
	if err != nil {
		t.Fatalf("Start with long TMPDIR: %v", err)
	}
	defer s.Close()
	if len(s.sock) > 100 {
		t.Fatalf("socket path too long: %d", len(s.sock))
	}
	if ans, err := Ask(s.sock, s.token, "p"); err != nil || ans != "ok" {
		t.Fatalf("ans=%q err=%v", ans, err)
	}
}

func TestAskRejectsMultilineAnswer(t *testing.T) {
	for _, a := range []string{"a\nb", "a\rb", "a\n"} {
		s, _ := Start(func(context.Context, string) (string, bool) { return a, true })
		if got, err := Ask(s.sock, s.token, "p"); err == nil {
			t.Errorf("answer %q forwarded as %q", a, got)
		}
		s.Close()
	}
}

func TestAskHandlerPanic(t *testing.T) {
	s, _ := Start(func(context.Context, string) (string, bool) { panic("boom") })
	defer s.Close()
	if _, err := Ask(s.sock, s.token, "p"); err == nil {
		t.Fatal("panicking handler produced an answer")
	}
	// The server must still be alive.
	if _, err := Ask(s.sock, s.token, "p"); err == nil {
		t.Fatal("second call should also be refused, not crash")
	}
}

func TestHandlerCtxCancelledWhenClientDisconnects(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	s, _ := Start(func(ctx context.Context, _ string) (string, bool) {
		close(started)
		<-ctx.Done()
		close(done)
		return "", false
	})
	defer s.Close()
	c, err := net.Dial("unix", s.sock)
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte(`{"token":"` + s.token + `","prompt":"p"}` + "\n"))
	<-started
	c.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler ctx not cancelled after client disconnect")
	}
}
