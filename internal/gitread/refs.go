// Package gitread reads refs, stashes and commits of a repository.
package gitread

import (
	"context"
	"strconv"
	"strings"

	"gitrais/internal/gitexec"
)

type RefKind string

const (
	RefLocal  RefKind = "local"
	RefRemote RefKind = "remote"
	RefTag    RefKind = "tag"
)

type Ref struct {
	Name     string  `json:"name"`  // refs/heads/main
	Short    string  `json:"short"` // main, origin/main, v1
	Kind     RefKind `json:"kind"`
	Remote   string  `json:"remote"`   // only for RefRemote
	Hash     string  `json:"hash"`     // commit (annotated tags are dereferenced)
	Upstream string  `json:"upstream"` // short form, e.g. origin/main
	Ahead    int     `json:"ahead"`
	Behind   int     `json:"behind"`
	Gone     bool    `json:"gone"`
	IsHead   bool    `json:"isHead"`
}

type Head struct {
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	Unborn   bool   `json:"unborn"`
	Hash     string `json:"hash"`
}

type Refs struct {
	Head Head  `json:"head"`
	Refs []Ref `json:"refs"`
}

// Fields are NUL-separated and records newline-separated: ref names cannot
// contain control characters, so neither separator can appear inside a field.
const refFormat = "--format=%(refname)%00%(objectname)%00%(*objectname)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(HEAD)"

func ReadRefs(ctx context.Context, r *gitexec.Runner, dir string) (Refs, error) {
	run := func(args ...string) (string, error) {
		out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: args, Env: gitexec.ReadOnlyEnv})
		return strings.TrimSpace(string(out)), err
	}
	var res Refs
	sym, err := run("symbolic-ref", "-q", "HEAD")
	switch {
	case err == nil:
		res.Head.Branch = strings.TrimPrefix(sym, "refs/heads/")
	case gitexec.ExitCode(err) == 1:
		res.Head.Detached = true
	default:
		return Refs{}, err
	}
	if h, err := run("rev-parse", "-q", "--verify", "HEAD"); err == nil {
		res.Head.Hash = h
	} else if gitexec.ExitCode(err) == 1 {
		res.Head.Unborn = true
	} else {
		return Refs{}, err
	}
	out, err := run("for-each-ref", refFormat, "refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return Refs{}, err
	}
	res.Refs = []Ref{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 6 || strings.HasSuffix(f[0], "/HEAD") && strings.HasPrefix(f[0], "refs/remotes/") {
			continue
		}
		ref := Ref{Name: f[0], Hash: f[1], Upstream: f[3], IsHead: f[5] == "*"}
		if f[2] != "" {
			ref.Hash = f[2]
		}
		ref.Ahead, ref.Behind, ref.Gone = parseTrack(f[4])
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			ref.Kind, ref.Short = RefLocal, strings.TrimPrefix(f[0], "refs/heads/")
		case strings.HasPrefix(f[0], "refs/remotes/"):
			ref.Kind, ref.Short = RefRemote, strings.TrimPrefix(f[0], "refs/remotes/")
			ref.Remote, _, _ = strings.Cut(ref.Short, "/")
		case strings.HasPrefix(f[0], "refs/tags/"):
			ref.Kind, ref.Short = RefTag, strings.TrimPrefix(f[0], "refs/tags/")
		}
		res.Refs = append(res.Refs, ref)
	}
	return res, nil
}

func parseTrack(s string) (ahead, behind int, gone bool) {
	if s == "gone" {
		return 0, 0, true
	}
	for _, part := range strings.Split(s, ", ") {
		k, v, ok := strings.Cut(part, " ")
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(v)
		switch k {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return
}
