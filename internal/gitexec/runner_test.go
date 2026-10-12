package gitexec_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

func TestRunStdout(t *testing.T) {
	r := testutil.Runner(t)
	out, err := r.Run(context.Background(), gitexec.Cmd{Args: []string{"--version"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "git version ") {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestRunNotARepo(t *testing.T) {
	r := testutil.Runner(t)
	_, err := r.Run(context.Background(), gitexec.Cmd{Dir: t.TempDir(), Args: []string{"rev-parse", "HEAD"}})
	var ge *gitexec.GitError
	if !errors.As(err, &ge) {
		t.Fatalf("want *GitError, got %T %v", err, err)
	}
	if ge.ExitCode != 128 || ge.Kind != gitexec.KindNotARepo {
		t.Fatalf("got exit=%d kind=%s stderr=%q", ge.ExitCode, ge.Kind, ge.Stderr)
	}
}

func TestRunEnvApplied(t *testing.T) {
	repo := testutil.NewRepo(t)
	out, err := repo.R.Run(context.Background(), gitexec.Cmd{
		Dir: repo.Dir, Args: []string{"var", "GIT_AUTHOR_IDENT"}, Env: []string{"GIT_AUTHOR_NAME=Zed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "Zed <") {
		t.Fatalf("per-call env not applied: %q", out)
	}
}

func TestRunStdin(t *testing.T) {
	repo := testutil.NewRepo(t)
	out, err := repo.R.Run(context.Background(), gitexec.Cmd{
		Dir: repo.Dir, Args: []string{"hash-object", "--stdin"}, Stdin: strings.NewReader("hello\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("hash = %s", got)
	}
}

func TestRunCancel(t *testing.T) {
	repo := testutil.NewRepo(t)
	pr, pw := io.Pipe()
	defer pw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := repo.R.Run(ctx, gitexec.Cmd{Dir: repo.Dir, Args: []string{"hash-object", "--stdin"}, Stdin: pr})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("cancel took too long")
	}
}

func TestStreamRecords(t *testing.T) {
	repo := testutil.NewRepo(t)
	for _, m := range []string{"a", "b", "c"} {
		repo.CommitAll(m)
	}
	var subjects []string
	err := repo.R.Stream(context.Background(), gitexec.Cmd{
		Dir: repo.Dir, Args: []string{"log", "--format=%s%x1e"},
	}, 0x1e, func(rec []byte) error {
		rec = bytes.TrimLeft(rec, "\n")
		if len(rec) > 0 {
			subjects = append(subjects, string(rec))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(subjects, ",") != "c,b,a" {
		t.Fatalf("subjects = %v", subjects)
	}
}

func TestStreamCallbackErrorStops(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("a")
	repo.CommitAll("b")
	stop := errors.New("stop")
	err := repo.R.Stream(context.Background(), gitexec.Cmd{Dir: repo.Dir, Args: []string{"log", "--format=%s%x1e"}},
		0x1e, func([]byte) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("want stop, got %v", err)
	}
}
