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
	"syscall"
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

// ReadOnlyEnv evita che i comandi di sola lettura (status, diff) prendano lock opzionali sull'index.
var ReadOnlyEnv = []string{"GIT_OPTIONAL_LOCKS=0"}

const maxStderr = 64 << 10

// maxRecord è la dimensione massima di un record di Stream (variabile per i test).
var maxRecord = 16 << 20

func (r *Runner) command(ctx context.Context, c Cmd) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.Git, c.Args...)
	cmd.Dir = c.Dir
	env := append(inheritedEnv(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	env = append(env, r.Env...)
	cmd.Env = append(env, c.Env...)
	switch in := c.Stdin.(type) {
	case nil:
	case *os.File:
		cmd.Stdin = in // exec passa il file direttamente al processo: nessuna goroutine di copia
	default:
		cmd.Stdin = &ctxReader{ctx: ctx, r: in}
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// redirectEnv elenca le variabili che reindirizzerebbero git su un altro repository.
var redirectEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX",
}

// inheritedEnv ritorna os.Environ() senza le variabili di redirectEnv, così una shell o un hook
// genitore non può far operare i comandi su un repository diverso da Cmd.Dir.
func inheritedEnv() []string {
	var out []string
envLoop:
	for _, kv := range os.Environ() {
		for _, k := range redirectEnv {
			if strings.HasPrefix(kv, k+"=") {
				continue envLoop
			}
		}
		out = append(out, kv)
	}
	return out
}

func (r *Runner) Run(ctx context.Context, c Cmd) ([]byte, error) {
	var stdout bytes.Buffer
	tail := &tailWriter{on: c.OnStderr}
	cmd := r.command(ctx, c)
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
	cmd := r.command(ctx2, c)
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
		cancel() // nessuno drena più stdout: senza cancel git resterebbe bloccato sulla pipe piena
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

// tailWriter conserva gli ultimi maxStderr byte e inoltra ogni chunk a on.
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

// ctxReader rende interrompibile la lettura di stdin: exec.Cmd.Wait aspetta la goroutine
// che copia stdin, e una Read bloccata su un reader non-file (es. io.Pipe) non verrebbe
// mai sbloccata dalla cancellazione del context. La goroutine di lettura sottostante può
// restare in sospeso finché il reader non ritorna, ma non blocca più Run/Stream.
// Attenzione: dopo la cancellazione una Read rimasta in sospeso può ancora consumare e
// scartare dati del chiamante. Gli *os.File non passano da qui (vedi command).
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
