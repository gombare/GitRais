package testutil

import "testing"

func TestNewRepoCommit(t *testing.T) {
	r := NewRepo(t)
	r.Write("a.txt", "x\n")
	h := r.CommitAll("first")
	if len(h) != 40 {
		t.Fatalf("hash %q", h)
	}
	if got := r.Git("log", "--format=%an <%ae>", "-1"); got != "Test <test@example.com>" {
		t.Fatalf("author %q", got)
	}
}
