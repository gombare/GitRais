package gitread

import (
	"context"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"gitrais/internal/gitexec"
)

// FileChange describes one file touched between two trees.
type FileChange struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath"`
	Status    string `json:"status"` // A M D R C T
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
}

// CommitDetail holds the metadata and changed files of a single commit.
type CommitDetail struct {
	Hash           string       `json:"hash"`
	Summary        string       `json:"summary"`
	Body           string       `json:"body"`
	Parents        []string     `json:"parents"`
	Author         string       `json:"author"`
	AuthorEmail    string       `json:"authorEmail"`
	AuthorTime     int64        `json:"authorTime"`
	Committer      string       `json:"committer"`
	CommitterEmail string       `json:"committerEmail"`
	CommitTime     int64        `json:"commitTime"`
	Files          []FileChange `json:"files"`
}

// OpenRepo opens the repository at dir (or a parent containing .git).
func OpenRepo(dir string) (*git.Repository, error) {
	return git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{DetectDotGit: true, EnableDotGitCommonDir: true})
}

// ReadCommit reads a commit's metadata via go-git and its changed files via
// git. Merges are compared with their first parent, root commits with the
// empty tree.
func ReadCommit(ctx context.Context, r *gitexec.Runner, repo *git.Repository, dir, hash string) (CommitDetail, error) {
	c, err := repo.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		return CommitDetail{}, err
	}
	msg := strings.TrimRight(c.Message, "\n")
	summary, body, _ := strings.Cut(msg, "\n")
	d := CommitDetail{
		Hash: c.Hash.String(), Summary: summary, Body: strings.TrimLeft(body, "\n"),
		Author: c.Author.Name, AuthorEmail: c.Author.Email, AuthorTime: c.Author.When.Unix(),
		Committer: c.Committer.Name, CommitterEmail: c.Committer.Email, CommitTime: c.Committer.When.Unix(),
		Parents: []string{},
	}
	for _, p := range c.ParentHashes {
		d.Parents = append(d.Parents, p.String())
	}
	var from string
	if len(d.Parents) > 0 {
		from = d.Parents[0]
	} else if from, err = EmptyTree(ctx, r, dir); err != nil {
		return CommitDetail{}, err
	}
	d.Files, err = ChangedFiles(ctx, r, dir, from, d.Hash)
	return d, err
}

// EmptyTree returns the hash of the empty tree object for the repository's
// hash format.
func EmptyTree(ctx context.Context, r *gitexec.Runner, dir string) (string, error) {
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"hash-object", "-t", "tree", "--stdin"},
		Stdin: strings.NewReader("")})
	return strings.TrimSpace(string(out)), err
}

// ChangedFiles lists the files changed between two tree-ish values, combining
// --name-status and --numstat. Both use -z, so paths are never quoted.
func ChangedFiles(ctx context.Context, r *gitexec.Runner, dir, from, to string) ([]FileChange, error) {
	base := []string{"diff", "--no-color", "--no-ext-diff", "-M", "-z"}
	ns, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Env: gitexec.ReadOnlyEnv, Args: append(append([]string{}, base...), "--name-status", "--end-of-options", from, to, "--")})
	if err != nil {
		return nil, err
	}
	num, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Env: gitexec.ReadOnlyEnv, Args: append(append([]string{}, base...), "--numstat", "--end-of-options", from, to, "--")})
	if err != nil {
		return nil, err
	}
	files := []FileChange{}
	tok := strings.Split(string(ns), "\x00")
	for i := 0; i+1 < len(tok); {
		st := tok[i]
		if st == "" {
			break
		}
		f := FileChange{Status: st[:1]}
		if f.Status == "R" || f.Status == "C" {
			if i+2 >= len(tok) {
				break
			}
			f.OldPath, f.Path = tok[i+1], tok[i+2]
			i += 3
		} else {
			f.Path = tok[i+1]
			i += 2
		}
		files = append(files, f)
	}
	stats := parseNumstat(string(num))
	for i := range files {
		if s, ok := stats[files[i].Path]; ok {
			files[i].Additions, files[i].Deletions, files[i].Binary = s.add, s.del, s.bin
		}
	}
	return files, nil
}

type numstat struct {
	add, del int
	bin      bool
}

// parseNumstat parses `--numstat -z`: "A\tD\tpath\0" or, for renames/copies,
// "A\tD\t\0old\0new\0". Results are keyed by the (new) path.
func parseNumstat(s string) map[string]numstat {
	res := map[string]numstat{}
	tok := strings.Split(s, "\x00")
	for i := 0; i < len(tok); i++ {
		parts := strings.SplitN(tok[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		ns := numstat{bin: parts[0] == "-"}
		ns.add, _ = strconv.Atoi(parts[0])
		ns.del, _ = strconv.Atoi(parts[1])
		path := parts[2]
		if path == "" && i+2 < len(tok) {
			path = tok[i+2]
			i += 2
		}
		res[path] = ns
	}
	return res
}
