package graph

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"gitrais/internal/gitexec"
)

type Stash struct {
	Hash, Base, Ref, Subject, Author, Email string
	Time                                    int64
}

type LoadOptions struct {
	Stashes    []Stash
	Batch      int
	OnProgress func(rows int)
}

// With -z records are NUL-separated; %x1f separates fields and the subject is last,
// so 0x1e/0x1f inside commit text stay in the subject.
const logFormat = "--format=%H%x1f%P%x1f%an%x1f%ae%x1f%at%x1f%s"

func Load(ctx context.Context, r *gitexec.Runner, dir string, idx *Index, opt LoadOptions) (err error) {
	defer func() { idx.finish(err) }()
	if opt.Batch <= 0 {
		opt.Batch = 5000
	}
	revs, err := startRevs(ctx, r, dir)
	if err != nil || len(revs) == 0 {
		return err
	}
	stashByBase := map[string][]Stash{}
	for _, s := range opt.Stashes {
		stashByBase[s.Base] = append(stashByBase[s.Base], s)
		// the base may be unreachable from refs: include it so the stash row gets placed
		revs = append(revs, s.Base)
	}
	var (
		layout   Layout
		batch    = make([]Row, 0, opt.Batch)
		intern   = map[string]string{}
		reported bool
	)
	in := func(s string) string {
		if v, ok := intern[s]; ok {
			return v
		}
		s = strings.Clone(s) // do not keep the whole record alive
		intern[s] = s
		return s
	}
	place := func(row Row) {
		p := layout.Next(Commit{Hash: row.Hash, Parents: row.Parents})
		row.Lane, row.Color, row.Up, row.Down = p.Lane, p.Color, p.Up, p.Down
		batch = append(batch, row)
		if len(batch) == opt.Batch {
			idx.append(batch)
			batch = batch[:0] // idx.append copies the rows: the slice can be reused
			reported = true
			if opt.OnProgress != nil {
				opt.OnProgress(idx.Len())
			}
		}
	}
	args := append([]string{"log", "-z", "--topo-order", "--no-color", logFormat}, revs...)
	err = r.Stream(ctx, gitexec.Cmd{Dir: dir, Args: args, Env: gitexec.ReadOnlyEnv}, 0, func(rec []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(rec) == 0 {
			return nil
		}
		f := strings.SplitN(string(rec), "\x1f", 6)
		if len(f) != 6 {
			return fmt.Errorf("unexpected git log record: %q", rec)
		}
		ts, perr := strconv.ParseInt(f[4], 10, 64)
		if perr != nil {
			return fmt.Errorf("invalid timestamp in commit %s: %w", f[0], perr)
		}
		// strings.Clone: rows must not keep the whole record alive
		parents := strings.Fields(f[1])
		for i, p := range parents {
			parents[i] = strings.Clone(p)
		}
		hash := strings.Clone(f[0])
		for _, s := range stashByBase[hash] {
			place(Row{Hash: s.Hash, Parents: []string{s.Base}, Kind: RowStash, StashRef: s.Ref,
				Subject: s.Subject, Author: in(s.Author), Email: in(s.Email), Time: s.Time})
		}
		place(Row{Hash: hash, Parents: parents, Author: in(f[2]), Email: in(f[3]), Time: ts, Subject: strings.Clone(f[5])})
		return nil
	})
	if err != nil {
		return err
	}
	if len(batch) > 0 {
		idx.append(batch)
		reported = false
	}
	if opt.OnProgress != nil && !reported {
		opt.OnProgress(idx.Len())
	}
	return nil
}

// startRevs returns the revision arguments; empty if the repo has no commits.
func startRevs(ctx context.Context, r *gitexec.Runner, dir string) ([]string, error) {
	revs := []string{"--branches", "--tags", "--remotes"}
	_, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "--verify", "-q", "HEAD"}, Env: gitexec.ReadOnlyEnv})
	hasHead := err == nil
	if err != nil && gitexec.ExitCode(err) != 1 {
		return nil, err
	}
	if hasHead {
		return append(revs, "HEAD"), nil
	}
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"for-each-ref", "--count=1", "--format=%(refname)",
		"refs/heads", "refs/tags", "refs/remotes"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	return revs, nil
}
