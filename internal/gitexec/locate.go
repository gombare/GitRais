// Package gitexec runs the bundled git binary and parses its output.
package gitexec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths of the bundled binaries, relative to the executable's directory in
// the .app bundle and to the repository root in a development tree.
var (
	gitBundleRel    = filepath.Join("..", "Resources", "git", "bin", "git")
	gitDevRel       = filepath.Join("build", "git-dist", "bin", "git")
	helperBundleRel = filepath.Join("..", "Resources", "askpass-helper")
	helperDevRel    = filepath.Join("build", "bin", "askpass-helper")
)

// Locate returns the path of the bundled git.
func Locate() (string, error) {
	return locate("GITRAIS_GIT", gitBundleRel, gitDevRel)
}

// LocateHelper returns the path of askpass-helper.
func LocateHelper() (string, error) {
	return locate("GITRAIS_ASKPASS_HELPER", helperBundleRel, helperDevRel)
}

func locate(envKey, bundleRel, devRel string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return locateFrom(exe, productionBuild, envKey, bundleRel, devRel)
}

// locateFrom finds a binary for the executable exe. In a packaged app (a
// production build running from a .app bundle) only the bundled copy is
// accepted: an inherited environment variable or a stray development tree
// must never redirect a shipped app to another git. Elsewhere (wails dev,
// which also runs from a .app, go test) envKey, then the bundle layout, then
// an upward search for devRel are tried.
func locateFrom(exe string, production bool, envKey, bundleRel, devRel string) (string, error) {
	if exe != "" {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		if production && inAppBundle(exe) {
			p := filepath.Clean(filepath.Join(filepath.Dir(exe), bundleRel))
			if isExecutable(p) {
				return p, nil
			}
			return "", fmt.Errorf("%s is missing from the app bundle", p)
		}
	}
	if p := os.Getenv(envKey); p != "" {
		if isExecutable(p) {
			return p, nil
		}
		return "", fmt.Errorf("%s=%q is not an executable", envKey, p)
	}
	if exe != "" {
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

// inAppBundle reports whether exe is the executable of a macOS app bundle
// (<name>.app/Contents/MacOS/<exe>).
func inAppBundle(exe string) bool {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	return filepath.Base(macOS) == "MacOS" && filepath.Base(contents) == "Contents" &&
		strings.HasSuffix(filepath.Dir(contents), ".app")
}
