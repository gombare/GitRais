package gitexec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Cmd struct {
	Dir      string
	Args     []string
	Stdin    io.Reader
	Env      []string
	OnStderr func([]byte)
}

type Runner struct {
	Git string
	Env []string
	Log *ActivityLog
}

// ReadOnlyEnv prevents read-only commands (status, diff) from taking optional locks on the index.
var ReadOnlyEnv = []string{"GIT_OPTIONAL_LOCKS=0"}

const maxStderr = 64 << 10

// maxRecord is the maximum size of a Stream record (a variable so tests can override it).
var maxRecord = 16 << 20

func (r *Runner) command(ctx context.Context, c Cmd) (*exec.Cmd, func()) {
	cmd := exec.CommandContext(ctx, r.Git, c.Args...)
	cmd.Dir = c.Dir
	env := append(inheritedEnv(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	env = append(env, r.Env...)
	cmd.Env = append(env, c.Env...)
	switch in := c.Stdin.(type) {
	case nil:
	case *os.File:
		cmd.Stdin = in // exec passes the file directly to the process: no copy goroutine
	default:
		cmd.Stdin = &ctxReader{ctx: ctx, r: in}
	}
	release := setProcessGroup(cmd)
	cmd.WaitDelay = 5 * time.Second
	return cmd, release
}

// redirectEnv lists the variables that would redirect git to another repository.
var redirectEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX",
}

// configEnv lists the variables that inject git configuration (git -c and
// GIT_CONFIG_COUNT with its GIT_CONFIG_KEY_<n>/GIT_CONFIG_VALUE_<n> pairs).
var configEnv = []string{"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT"}

// inheritedEnv returns os.Environ() without the redirectEnv and configEnv variables, so a parent
// shell or hook can neither make commands operate on a repository other than Cmd.Dir nor inject
// configuration (core.fsmonitor, core.sshCommand, ...) into them.
func inheritedEnv() []string {
	var out []string
envLoop:
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_") {
			continue
		}
		for _, drop := range [][]string{redirectEnv, configEnv} {
			for _, d := range drop {
				if k == d {
					continue envLoop
				}
			}
		}
		out = append(out, kv)
	}
	return out
}

func (r *Runner) Run(ctx context.Context, c Cmd) ([]byte, error) {
	var stdout bytes.Buffer
	tail := &tailWriter{on: c.OnStderr}
	cmd, release := r.command(ctx, c)
	defer release()
	cmd.Stdout = &stdout
	cmd.Stderr = tail
	start := time.Now()
	err := cmd.Run()
	return stdout.Bytes(), r.finish(ctx, c, start, err, tail)
}

func (r *Runner) Stream(ctx context.Context, c Cmd, delim byte, onRecord func([]byte) error) error {
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()
	tail := &tailWriter{on: c.OnStderr}
	cmd, release := r.command(ctx2, c)
	defer release()
	cmd.Stderr = tail
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), maxRecord)
	sc.Split(splitOn(delim))
	var cbErr error
	for sc.Scan() {
		if cbErr = onRecord(sc.Bytes()); cbErr != nil {
			cancel()
			break
		}
	}
	scanErr := sc.Err()
	if scanErr != nil {
		cancel() // nothing drains stdout anymore: without cancel, git would stay blocked on the full pipe
	}
	waitErr := cmd.Wait()
	if cbErr != nil {
		return cbErr
	}
	if scanErr != nil && ctx.Err() == nil {
		return scanErr
	}
	return r.finish(ctx, c, start, waitErr, tail)
}

func (r *Runner) finish(ctx context.Context, c Cmd, start time.Time, err error, tail *tailWriter) error {
	exit := 0
	if err != nil {
		exit = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		}
	}
	r.Log.Record(c.Dir, c.Args, time.Since(start), exit)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return newGitError(c.Args, exit, tail.String(), err)
}

func splitOn(delim byte) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, delim); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
}

// tailWriter keeps the last maxStderr bytes and forwards each chunk to on.
type tailWriter struct {
	buf []byte
	on  func([]byte)
}

func (w *tailWriter) Write(p []byte) (int, error) {
	if w.on != nil {
		w.on(p)
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) > maxStderr {
		w.buf = w.buf[len(w.buf)-maxStderr:]
	}
	return len(p), nil
}

func (w *tailWriter) String() string { return string(w.buf) }

// ctxReader makes reading stdin interruptible: exec.Cmd.Wait waits for the goroutine copying
// stdin, and a Read blocked on a non-file reader (e.g. io.Pipe) would never be unblocked by
// context cancellation. The underlying read goroutine may stay pending until the reader
// returns, but it no longer blocks Run/Stream. Warning: after cancellation, a pending Read may
// still consume and discard data from the caller. *os.File values do not go through here (see command).
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	type res struct {
		n   int
		err error
		buf []byte
	}
	ch := make(chan res, 1)
	buf := make([]byte, len(p))
	go func() {
		n, err := c.r.Read(buf)
		ch <- res{n, err, buf}
	}()
	select {
	case r := <-ch:
		copy(p, r.buf[:r.n])
		return r.n, r.err
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	}
}
