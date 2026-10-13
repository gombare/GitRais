package gitread

import (
	"context"
	"strconv"
	"strings"

	"gitrais/internal/gitexec"
	"gitrais/internal/graph"
)

// ReadStashes lists stashes. Records are NUL-terminated (-z) and fields are
// separated by 0x1f, with the free-text subject last, so any subject text is safe.
func ReadStashes(ctx context.Context, r *gitexec.Runner, dir string) ([]graph.Stash, error) {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Env: gitexec.ReadOnlyEnv, Args: []string{
		"stash", "list", "--no-show-signature", "-z", "--format=%H%x1f%P%x1f%gd%x1f%at%x1f%an%x1f%ae%x1f%gs"}})
	if err != nil {
		return nil, err
	}
	stashes := []graph.Stash{}
	for _, rec := range strings.Split(string(out), "\x00") {
		rec = strings.TrimPrefix(rec, "\n")
		f := strings.SplitN(rec, "\x1f", 7)
		if len(f) != 7 {
			continue
		}
		parents := strings.Fields(f[1])
		if len(parents) == 0 {
			continue
		}
		ts, _ := strconv.ParseInt(f[3], 10, 64)
		stashes = append(stashes, graph.Stash{Hash: f[0], Base: parents[0], Ref: f[2], Subject: f[6],
			Time: ts, Author: f[4], Email: f[5]})
	}
	return stashes, nil
}
