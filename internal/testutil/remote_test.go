package testutil

import "testing"

func TestBareRemotePushFetch(t *testing.T) {
	a := NewRepo(t)
	bare := NewBare(t, a.R)
	a.CommitAll("one")
	a.Git("remote", "add", "origin", bare)
	a.Git("push", "-q", "-u", "origin", "main")
	b := NewRepoWith(t, a.R)
	b.Git("remote", "add", "origin", bare)
	b.Git("fetch", "-q", "origin")
	if got, want := b.Git("rev-parse", "origin/main"), a.Git("rev-parse", "HEAD"); got != want {
		t.Fatalf("origin/main=%s want %s", got, want)
	}
}
