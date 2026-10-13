package ops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// WorktreePath resolves rel (a clean path relative to the repository root, as
// git status and diff report it; ErrInvalidFilePath otherwise) to an absolute
// path inside the working tree of dir, for showing it in the file manager.
//
// No symbolic link is followed: a path crossing a symlinked directory is
// refused (it could lead outside the repository), while a symlink as the last
// component is returned as is. When the path does not exist (e.g. a deleted
// file) the nearest existing ancestor is returned instead.
func WorktreePath(dir, rel string) (string, error) {
	if err := checkRelPath(rel); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	parts := strings.Split(rel, "/")
	cur := root
	for i, c := range parts {
		next := filepath.Join(cur, c)
		fi, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return cur, nil
		}
		if err != nil {
			return "", err
		}
		last := i == len(parts)-1
		if !last && fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %q crosses a symbolic link", ErrInvalidFilePath, rel)
		}
		if !last && !fi.IsDir() {
			return cur, nil // a file where a directory is expected: the path does not exist
		}
		cur = next
	}
	return cur, nil
}
