// Package status parses `git status --porcelain=v2 --branch -z` output.
package status

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gitrais/internal/gitexec"
)

type FileStatus struct {
	Path       string `json:"path"`
	OrigPath   string `json:"origPath"`
	Index      string `json:"index"`
	WorkTree   string `json:"workTree"`
	Conflicted bool   `json:"conflicted"`
	Untracked  bool   `json:"untracked"`
}

type Status struct {
	Branch   string       `json:"branch"`
	Detached bool         `json:"detached"`
	Unborn   bool         `json:"unborn"`
	Upstream string       `json:"upstream"`
	Ahead    int          `json:"ahead"`
	Behind   int          `json:"behind"`
	Files    []FileStatus `json:"files"`
}

func Read(ctx context.Context, r *gitexec.Runner, dir string) (Status, error) {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Env: gitexec.ReadOnlyEnv,
		Args: []string{"status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all"}})
	if err != nil {
		return Status{}, err
	}
	return Parse(out)
}

func Parse(data []byte) (Status, error) {
	s := Status{Files: []FileStatus{}}
	tok := strings.Split(string(data), "\x00")
	for i := 0; i < len(tok); i++ {
		e := tok[i]
		if e == "" {
			continue
		}
		switch e[0] {
		case '#':
			parseHeader(&s, e)
			// '!' (ignored) and unknown record types are skipped.
		case '1':
			p := strings.SplitN(e, " ", 9)
			if len(p) != 9 {
				return s, fmt.Errorf("status: malformed entry %q", e)
			}
			if len(p[1]) != 2 || p[8] == "" {
				return s, fmt.Errorf("status: malformed entry %q", e)
			}
			s.Files = append(s.Files, FileStatus{Path: p[8], Index: p[1][:1], WorkTree: p[1][1:]})
		case '2':
			p := strings.SplitN(e, " ", 10)
			if len(p) != 10 || i+1 >= len(tok) {
				return s, fmt.Errorf("status: malformed entry %q", e)
			}
			if len(p[1]) != 2 || p[9] == "" || tok[i+1] == "" {
				return s, fmt.Errorf("status: malformed entry %q", e)
			}
			i++
			s.Files = append(s.Files, FileStatus{Path: p[9], OrigPath: tok[i], Index: p[1][:1], WorkTree: p[1][1:]})
		case 'u':
			p := strings.SplitN(e, " ", 11)
			if len(p) != 11 || len(p[1]) != 2 || p[10] == "" {
				return s, fmt.Errorf("status: malformed entry %q", e)
			}
			// keep the real conflict code (AA, DU, UD, ...) so the UI can offer the right resolution
			s.Files = append(s.Files, FileStatus{Path: p[10], Index: p[1][:1], WorkTree: p[1][1:], Conflicted: true})
		case '?':
			if len(e) < 3 || e[1] != ' ' {
				return s, fmt.Errorf("status: malformed entry %q", e)
			}
			s.Files = append(s.Files, FileStatus{Path: e[2:], Index: ".", WorkTree: "?", Untracked: true})
		}
	}
	return s, nil
}

func parseHeader(s *Status, e string) {
	switch {
	case strings.HasPrefix(e, "# branch.oid "):
		s.Unborn = strings.TrimPrefix(e, "# branch.oid ") == "(initial)"
	case strings.HasPrefix(e, "# branch.head "):
		h := strings.TrimPrefix(e, "# branch.head ")
		if h == "(detached)" {
			s.Detached = true
		} else {
			s.Branch = h
		}
	case strings.HasPrefix(e, "# branch.upstream "):
		s.Upstream = strings.TrimPrefix(e, "# branch.upstream ")
	case strings.HasPrefix(e, "# branch.ab "):
		var a, b string
		fmt.Sscan(strings.TrimPrefix(e, "# branch.ab "), &a, &b)
		s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(a, "+"))
		s.Behind, _ = strconv.Atoi(strings.TrimPrefix(b, "-"))
	}
}

func (s Status) Staged() []FileStatus {
	out := []FileStatus{}
	for _, f := range s.Files {
		if f.Index != "." && !f.Untracked && !f.Conflicted {
			out = append(out, f)
		}
	}
	return out
}

func (s Status) Unstaged() []FileStatus {
	out := []FileStatus{}
	for _, f := range s.Files {
		if f.WorkTree != "." || f.Untracked || f.Conflicted {
			out = append(out, f)
		}
	}
	return out
}
