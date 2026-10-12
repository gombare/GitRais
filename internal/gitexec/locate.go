// Package gitexec runs the bundled git binary and parses its output.
package gitexec

import (
	"fmt"
	"os"
	"path/filepath"
)

// Locate returns the path of the bundled git.
func Locate() (string, error) {
	return locate("GITRAIS_GIT",
		filepath.Join("..", "Resources", "git", "bin", "git"),
		filepath.Join("build", "git-dist", "bin", "git"))
}

// LocateHelper returns the path of askpass-helper.
func LocateHelper() (string, error) {
	return locate("GITRAIS_ASKPASS_HELPER",
		filepath.Join("..", "Resources", "askpass-helper"),
		filepath.Join("build", "bin", "askpass-helper"))
}

func locate(envKey, bundleRel, devRel string) (string, error) {
	if p := os.Getenv(envKey); p != "" {
		if isExecutable(p) {
			return p, nil
		}
		return "", fmt.Errorf("%s=%q is not an executable", envKey, p)
	}
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		if p := filepath.Join(filepath.Dir(exe), bundleRel); isExecutable(p) {
			return filepath.Clean(p), nil
		}
		if p, ok := findUp(filepath.Dir(exe), devRel); ok {
			return p, nil
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if p, ok := findUp(wd, devRel); ok {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found: run `make git`/`make helper` or set %s", devRel, envKey)
}

func findUp(start, rel string) (string, bool) {
	dir := start
	for {
		p := filepath.Join(dir, rel)
		if isExecutable(p) {
			return p, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
