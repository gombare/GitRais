package gitread

import (
	"context"
	"testing"

	"gitrais/internal/testutil"
)

func TestParseTrack(t *testing.T) {
	cases := map[string][3]int{"": {0, 0, 0}, "ahead 3": {3, 0, 0}, "behind 2": {0, 2, 0},
		"ahead 1, behind 4": {1, 4, 0}, "gone": {0, 0, 1}}
	for in, w := range cases {
		a, b, g := parseTrack(in)
		gi := 0
		if g {
			gi = 1
		}
		if a != w[0] || b != w[1] || gi != w[2] {
			t.Errorf("parseTrack(%q) = %d,%d,%v", in, a, b, g)
		}
	}
}

func TestReadRefs(t *testing.T) {
	repo := testutil.NewRepo(t)
	base := repo.CommitAll("base")
	repo.Git("tag", "-a", "v1", "-m", "release")
	bare := testutil.NewBare(t, repo.R)
	repo.Git("remote", "add", "origin", bare)
	repo.Git("push", "-q", "-u", "origin", "main")
	repo.Git("remote", "set-head", "origin", "main")
	repo.Git("branch", "feature")
	local := repo.CommitAll("local only")

	refs, err := ReadRefs(context.Background(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if refs.Head.Branch != "main" || refs.Head.Detached || refs.Head.Hash != local {
		t.Fatalf("head = %+v", refs.Head)
	}
	by := map[string]Ref{}
	for _, r := range refs.Refs {
		by[r.Name] = r
	}
	m := by["refs/heads/main"]
	if !m.IsHead || m.Upstream != "origin/main" || m.Ahead != 1 || m.Behind != 0 || m.Kind != RefLocal {
		t.Fatalf("main = %+v", m)
	}
	if by["refs/heads/feature"].Hash != base {
		t.Fatalf("feature = %+v", by["refs/heads/feature"])
	}
	if tg := by["refs/tags/v1"]; tg.Hash != base || tg.Short != "v1" || tg.Kind != RefTag {
		t.Fatalf("tag = %+v (must be dereferenced to the commit)", tg)
	}
	if rm := by["refs/remotes/origin/main"]; rm.Short != "origin/main" || rm.Remote != "origin" || rm.Kind != RefRemote {
		t.Fatalf("remote = %+v", rm)
	}
	if _, ok := by["refs/remotes/origin/HEAD"]; ok {
		t.Fatal("origin/HEAD must be excluded")
	}
}

func TestReadRefsRemoteWithSlash(t *testing.T) {
	repo := testutil.NewRepo(t)
	h := repo.CommitAll("base")
	// "git remote add" refuses overlapping names, so write the config directly.
	repo.Git("config", "remote.up.url", "/nonexistent/a")
	repo.Git("config", "remote.up/stream.url", "/nonexistent/b")
	repo.Git("update-ref", "refs/remotes/up/dev", h)
	repo.Git("update-ref", "refs/remotes/up/stream/main", h)

	refs, err := ReadRefs(context.Background(), repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Ref{}
	for _, r := range refs.Refs {
		by[r.Name] = r
	}
	if r := by["refs/remotes/up/dev"]; r.Remote != "up" || r.Short != "up/dev" {
		t.Fatalf("up/dev = %+v", r)
	}
	if r := by["refs/remotes/up/stream/main"]; r.Remote != "up/stream" || r.Short != "up/stream/main" {
		t.Fatalf("up/stream/main = %+v", r)
	}
}

func TestReadRefsDetachedAndUnborn(t *testing.T) {
	repo := testutil.NewRepo(t)
	refs, err := ReadRefs(context.Background(), repo.R, repo.Dir)
	if err != nil || !refs.Head.Unborn || refs.Head.Branch != "main" {
		t.Fatalf("unborn: %+v %v", refs.Head, err)
	}
	h := repo.CommitAll("one")
	repo.Git("switch", "-q", "--detach", h)
	refs, err = ReadRefs(context.Background(), repo.R, repo.Dir)
	if err != nil || !refs.Head.Detached || refs.Head.Hash != h {
		t.Fatalf("detached: %+v %v", refs.Head, err)
	}
}

func TestReadStashes(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	base := repo.CommitAll("base")
	if s, err := ReadStashes(context.Background(), repo.R, repo.Dir); err != nil || len(s) != 0 {
		t.Fatalf("no stash expected: %v %v", s, err)
	}
	repo.Write("a.txt", "2\n")
	repo.Git("stash", "push", "-q", "-m", "wip one")
	s, err := ReadStashes(context.Background(), repo.R, repo.Dir)
	if err != nil || len(s) != 1 {
		t.Fatalf("%v %v", s, err)
	}
	if s[0].Base != base || s[0].Ref != "stash@{0}" || s[0].Subject != "On main: wip one" || s[0].Author != "Test" {
		t.Fatalf("stash = %+v", s[0])
	}
}
