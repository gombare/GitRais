package gitexec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateEnvOverride(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "git")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITRAIS_GIT", fake)
	got, err := Locate()
	if err != nil || got != fake {
		t.Fatalf("Locate() = %q, %v; want %q", got, err, fake)
	}
}

func TestLocateEnvMissingFile(t *testing.T) {
	t.Setenv("GITRAIS_GIT", "/nonexistent/git")
	if _, err := Locate(); err == nil {
		t.Fatal("expected error for missing GITRAIS_GIT target")
	}
}

func TestLocateWalksUpToBuildDir(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "build", "git-dist", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := findUp(sub, filepath.Join("build", "git-dist", "bin", "git"))
	if !ok || got != filepath.Join(bin, "git") {
		t.Fatalf("findUp = %q, %v", got, ok)
	}
}

func writeExe(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Inside a packaged .app only the bundled binaries are used: environment
// overrides and the development search are ignored.
func TestLocatePackagedAppUsesBundleOnly(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	exe := filepath.Join(root, "Gitrais.app", "Contents", "MacOS", "gitrais")
	writeExe(t, exe)
	bundled := filepath.Join(root, "Gitrais.app", "Contents", "Resources", "git", "bin", "git")
	writeExe(t, bundled)
	writeExe(t, filepath.Join(root, "build", "git-dist", "bin", "git")) // dev tree around the app
	override := filepath.Join(t.TempDir(), "git")
	writeExe(t, override)
	t.Setenv("GITRAIS_GIT", override)

	// A development build (wails dev) also runs from a .app: overrides apply.
	if got, err := locateFrom(exe, false, "GITRAIS_GIT", gitBundleRel, gitDevRel); err != nil || got != override {
		t.Fatalf("dev .app locate = %q, %v; want %q", got, err, override)
	}
	got, err := locateFrom(exe, true, "GITRAIS_GIT", gitBundleRel, gitDevRel)
	if err != nil || got != bundled {
		t.Fatalf("packaged locate = %q, %v; want %q", got, err, bundled)
	}
	// A broken bundle is an error, not a fallback to the environment or dev tree.
	if err := os.Remove(bundled); err != nil {
		t.Fatal(err)
	}
	if got, err := locateFrom(exe, true, "GITRAIS_GIT", gitBundleRel, gitDevRel); err == nil {
		t.Fatalf("packaged locate without bundled git = %q", got)
	}
}

// Outside a bundle the environment override and the upward search still work,
// even in a production build.
func TestLocateDevFallbacks(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	exe := filepath.Join(root, "build", "bin", "gitrais")
	writeExe(t, exe)
	dev := filepath.Join(root, "build", "git-dist", "bin", "git")
	writeExe(t, dev)
	t.Setenv("GITRAIS_GIT", "")
	if got, err := locateFrom(exe, true, "GITRAIS_GIT", gitBundleRel, gitDevRel); err != nil || got != dev {
		t.Fatalf("dev locate = %q, %v; want %q", got, err, dev)
	}
	override := filepath.Join(t.TempDir(), "git")
	writeExe(t, override)
	t.Setenv("GITRAIS_GIT", override)
	if got, err := locateFrom(exe, true, "GITRAIS_GIT", gitBundleRel, gitDevRel); err != nil || got != override {
		t.Fatalf("override locate = %q, %v; want %q", got, err, override)
	}
}
