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
