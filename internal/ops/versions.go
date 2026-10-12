package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"gitrais/internal/gitexec"
)

// VersionsRequest selects the two sides of a file diff. With Commit set the
// sides are the commit's first parent (at OldPath) and the commit; otherwise
// they are the WIP sides used by WipDiff: index → worktree (unstaged),
// HEAD → index (Staged) or nothing → worktree (Untracked).
type VersionsRequest struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath"` // pre-rename path for the old side ("" = Path)
	Staged    bool   `json:"staged"`
	Untracked bool   `json:"untracked"`
	Commit    string `json:"commit"` // full hash: the commit against its first parent
}

// Versions holds the old and new contents of a file. Binary and TooLarge
// leave both contents empty.
type Versions struct {
	Old      string `json:"old"`
	New      string `json:"new"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"tooLarge"`
}

// MaxDiffBytes caps the size of each side returned by FileVersions.
const MaxDiffBytes = 5 << 20

// binaryProbe is how many leading bytes git inspects for a NUL to decide
// that a buffer is binary.
const binaryProbe = 8000

// ErrInvalidFilePath is returned for a path that is not a plain path relative
// to the repository root (absolute, "..", ".", empty components, NUL or
// newline), that has a ".git" component (any case), or that crosses a
// symbolic link.
var ErrInvalidFilePath = errors.New("invalid file path")

var fullHashRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// side is one version of a file: its content, or a marker that it is too large.
type side struct {
	data     []byte
	tooLarge bool
}

// FileVersions returns the contents git diffs for one file: raw blobs for the
// index and commits (no textconv, no filters, like WipDiff), and the raw file
// for the worktree. A path absent from a tree, the index or the worktree reads
// as empty, and so does the parent of a root commit.
func FileVersions(ctx context.Context, r *gitexec.Runner, dir string, req VersionsRequest) (Versions, error) {
	oldPath := req.OldPath
	if oldPath == "" {
		oldPath = req.Path
	}
	if err := checkRelPath(req.Path); err != nil {
		return Versions{}, err
	}
	if err := checkRelPath(oldPath); err != nil {
		return Versions{}, err
	}
	var oldS, newS side
	var err error
	switch {
	case req.Commit != "":
		if !fullHashRe.MatchString(req.Commit) {
			return Versions{}, fmt.Errorf("invalid commit hash %q", req.Commit)
		}
		var s []side
		if s, err = readObjects(ctx, r, dir, req.Commit+"^1:"+oldPath, req.Commit+":"+req.Path); err == nil {
			oldS, newS = s[0], s[1]
		}
	case req.Untracked:
		newS, err = readWorktree(ctx, r, dir, req.Path)
	case req.Staged:
		var s []side
		if s, err = readObjects(ctx, r, dir, "HEAD:"+oldPath, ":0:"+req.Path); err == nil {
			oldS, newS = s[0], s[1]
		}
	default:
		var s []side
		if s, err = readObjects(ctx, r, dir, ":0:"+req.Path); err == nil {
			oldS = s[0]
			newS, err = readWorktree(ctx, r, dir, req.Path)
		}
	}
	if err != nil {
		return Versions{}, err
	}
	switch {
	case oldS.tooLarge || newS.tooLarge:
		return Versions{TooLarge: true}, nil
	case isBinary(oldS.data) || isBinary(newS.data):
		return Versions{Binary: true}, nil
	}
	return Versions{Old: string(oldS.data), New: string(newS.data)}, nil
}

// checkRelPath accepts only clean, slash-separated paths relative to the
// repository root, as reported by git status and diff. A ".git" component
// in any case is refused: git never tracks one, and on case-insensitive
// filesystems ".GIT" is the repository's own directory.
func checkRelPath(p string) error {
	if p == "" || strings.ContainsAny(p, "\x00\n\r") || strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: %q", ErrInvalidFilePath, p)
	}
	for _, c := range strings.Split(p, "/") {
		if c == "" || c == "." || c == ".." || strings.EqualFold(c, ".git") {
			return fmt.Errorf("%w: %q", ErrInvalidFilePath, p)
		}
	}
	return nil
}

// readObjects resolves object specs, one per line on stdin so they can never be
// taken as options (index entries are spelled ":0:<path>" so a path like "1:x"
// is never read as a stage number), and returns their contents. Missing objects and trees
// read as empty; a gitlink (submodule) reads as git prints it in a diff.
func readObjects(ctx context.Context, r *gitexec.Runner, dir string, specs ...string) ([]side, error) {
	in := strings.Join(specs, "\n") + "\n"
	out, err := r.Run(ctx, gitexec.Cmd{
		Dir:   dir,
		Args:  []string{"cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)"},
		Stdin: strings.NewReader(in),
		Env:   gitexec.ReadOnlyEnv,
	})
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(specs) {
		return nil, fmt.Errorf("cat-file: unexpected output %q", out)
	}
	res := make([]side, len(specs))
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) != 3 || !isHex(f[0]) {
			if strings.HasSuffix(line, " missing") {
				continue
			}
			return nil, fmt.Errorf("cat-file: %s", line)
		}
		size, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cat-file: %s", line)
		}
		switch f[1] {
		case "blob":
			if size > MaxDiffBytes {
				res[i].tooLarge = true
				continue
			}
			// f[0] is a hex object name: it cannot be taken as an option.
			if res[i].data, err = r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"cat-file", "blob", f[0]}, Env: gitexec.ReadOnlyEnv}); err != nil {
				return nil, err
			}
		case "commit":
			res[i].data = subproject(f[0])
		}
	}
	return res, nil
}

func isHex(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func subproject(hash string) []byte { return []byte("Subproject commit " + hash + "\n") }

// readWorktree reads a worktree file without following symbolic links: a
// symlink reads as its target path (what git diffs), a path crossing a
// symlinked directory is refused, a missing file reads as empty and a
// directory (a submodule checkout) reads as its HEAD commit.
func readWorktree(ctx context.Context, r *gitexec.Runner, dir, rel string) (side, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return side{}, err
	}
	parts := strings.Split(rel, "/")
	cur := root
	for i, c := range parts {
		cur = filepath.Join(cur, c)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return side{}, nil
		}
		if err != nil {
			return side{}, err
		}
		last := i == len(parts)-1
		switch mode := fi.Mode(); {
		case mode&fs.ModeSymlink != 0:
			if !last {
				return side{}, fmt.Errorf("%w: %q crosses a symbolic link", ErrInvalidFilePath, rel)
			}
			target, err := os.Readlink(cur)
			return side{data: []byte(target)}, err
		case !last && !mode.IsDir():
			// A file where a directory is expected: the path does not exist.
			return side{}, nil
		case last && mode.IsDir():
			return readSubmoduleHead(ctx, r, cur)
		case last && !mode.IsRegular():
			return side{}, fmt.Errorf("%w: %q is not a regular file", ErrInvalidFilePath, rel)
		case last && fi.Size() > MaxDiffBytes:
			return side{tooLarge: true}, nil
		}
	}
	return readRegular(cur)
}

// readRegular reads at most MaxDiffBytes+1 bytes of a regular file, refusing
// a file swapped for a symlink or a special file after it was checked.
func readRegular(p string) (side, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return side{}, nil
	}
	if err != nil {
		return side{}, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil {
		return side{}, err
	} else if !fi.Mode().IsRegular() {
		return side{}, fmt.Errorf("%w: %q is not a regular file", ErrInvalidFilePath, p)
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxDiffBytes+1))
	if err != nil {
		return side{}, err
	}
	if len(b) > MaxDiffBytes {
		return side{tooLarge: true}, nil
	}
	return side{data: b}, nil
}

// readSubmoduleHead returns the HEAD of a submodule checkout as git prints it
// in a diff; a plain directory (or an unborn submodule) reads as empty.
func readSubmoduleHead(ctx context.Context, r *gitexec.Runner, dir string) (side, error) {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
		return side{}, nil
	}
	out, err := r.Run(ctx, gitexec.Cmd{Dir: dir, Args: []string{"rev-parse", "-q", "--verify", "HEAD"}, Env: gitexec.ReadOnlyEnv})
	if err != nil {
		return side{}, nil
	}
	return side{data: subproject(strings.TrimSpace(string(out)))}, nil
}

// isBinary applies git's heuristic: a NUL in the first 8000 bytes.
func isBinary(b []byte) bool {
	if len(b) > binaryProbe {
		b = b[:binaryProbe]
	}
	return bytes.IndexByte(b, 0) >= 0
}
