// Package genrepo writes a synthetic history as a git fast-import stream, used
// to benchmark the graph loader on large repositories.
package genrepo

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"

	"gitrais/internal/gitexec"
)

// Validate checks the generator parameters.
func Validate(n, branches int) error {
	if n < 1 {
		return errors.New("commit count must be at least 1")
	}
	if branches < 1 {
		return errors.New("branch count must be at least 1")
	}
	if branches > 1000 {
		return errors.New("branch count must be at most 1000")
	}
	return nil
}

// Write emits n commits spread over branches b0..b{branches-1} with periodic
// merges: every 50th commit on a side branch merges b0 into it, and every 37th
// commit on b0 merges a random side branch. Each commit touches one of 1000
// tiny files. refs/heads/main points at the tip of b0. The output is
// deterministic for given parameters.
func Write(out io.Writer, n, branches int) error {
	if err := Validate(n, branches); err != nil {
		return err
	}
	w := bufio.NewWriterSize(out, 1<<20)
	rng := rand.New(rand.NewSource(1))
	tips := make([]int, branches) // mark of each branch tip (0 = not created yet)
	ts := int64(1_600_000_000)
	for i := 1; i <= n; i++ {
		b := 0
		if i > 1 {
			b = rng.Intn(branches)
		}
		ts += 60
		msg := fmt.Sprintf("commit %d on b%d", i, b)
		fmt.Fprintf(w, "commit refs/heads/b%d\nmark :%d\ncommitter Gen <gen@example.com> %d +0000\ndata %d\n%s\n", b, i, ts, len(msg), msg)
		created := tips[b] == 0 && i > 1
		if created {
			fmt.Fprintf(w, "from :%d\n", tips[0])
		}
		// A new branch already starts at b0's tip: merging it would duplicate the parent.
		if b != 0 && !created && i%50 == 0 && tips[0] != 0 {
			fmt.Fprintf(w, "merge :%d\n", tips[0])
		}
		if b == 0 && branches > 1 && i%37 == 0 {
			if o := rng.Intn(branches-1) + 1; tips[o] != 0 {
				fmt.Fprintf(w, "merge :%d\n", tips[o])
			}
		}
		content := fmt.Sprintf("%d\n", i)
		fmt.Fprintf(w, "M 644 inline f%d.txt\ndata %d\n%s\n", i%1000, len(content), content)
		tips[b] = i
	}
	fmt.Fprintf(w, "reset refs/heads/main\nfrom :%d\n\n", tips[0])
	return w.Flush()
}

// Create initialises a repository in dir (which must not contain one), imports
// the generated history, points HEAD at main and writes the commit-graph file
// (as `git gc` would) so that `git log --topo-order` streams incrementally.
func Create(ctx context.Context, r *gitexec.Runner, dir string, n, branches int) error {
	if err := Validate(n, branches); err != nil {
		return err
	}
	if _, err := r.Run(ctx, gitexec.Cmd{Args: []string{"init", "-q", dir}}); err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(Write(pw, n, branches)) }()
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"fast-import", "--quiet"}, Stdin: pr})
	pr.CloseWithError(errors.New("fast-import exited")) // unblock the writer if fast-import failed early
	if err != nil {
		return err
	}
	for _, args := range [][]string{
		{"symbolic-ref", "HEAD", "refs/heads/main"},
		{"commit-graph", "write", "--reachable"},
	} {
		if _, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: args}); err != nil {
			return err
		}
	}
	return nil
}
