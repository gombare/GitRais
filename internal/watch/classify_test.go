package watch

import "testing"

func TestClassify(t *testing.T) {
	ign := func(rel string) bool { return rel == "node_modules" || len(rel) > 13 && rel[:13] == "node_modules/" }
	cases := map[string]Kind{
		"/r/a.txt":                     WIP,
		"/r/src/x.go":                  WIP,
		"/r/node_modules/p/i.js":       0,
		"/r/.git/index":                WIP,
		"/r/.git/index.lock":           0,
		"/r/.git/HEAD":                 Refs | WIP,
		"/r/.git/refs/heads/main":      Refs,
		"/r/.git/refs/heads/main.lock": 0,
		"/r/.git/packed-refs":          Refs,
		"/r/.git/FETCH_HEAD":           Refs,
		"/r/.git/objects/ab/cdef":      0,
		"/r/.git/logs/HEAD":            0,
		"/other/file":                  0,
	}
	for p, want := range cases {
		if got := Classify("/r", "/r/.git", p, ign); got != want {
			t.Errorf("Classify(%s) = %d, want %d", p, got, want)
		}
	}
}

func TestClassifyExtra(t *testing.T) {
	ign := func(rel string) bool { return rel == "build" || rel == "build/out" }
	cases := []struct {
		name, root, gitDir, path string
		want                     Kind
	}{
		{"stash ref", "/r", "/r/.git", "/r/.git/refs/stash", Refs},
		{"orig head", "/r", "/r/.git", "/r/.git/ORIG_HEAD", Refs},
		{"refs dir (coalesced)", "/r", "/r/.git/", "/r/.git/refs/heads", Refs},
		{"refs root dir", "/r", "/r/.git", "/r/.git/refs", Refs},
		{"git dir itself (coalesced)", "/r", "/r/.git", "/r/.git", Refs | WIP},
		{"git dir itself, trailing slash", "/r", "/r/.git", "/r/.git/", Refs | WIP},
		{"config", "/r", "/r/.git", "/r/.git/config", Refs},
		{"config lock", "/r", "/r/.git", "/r/.git/config.lock", 0},
		{"info/exclude", "/r", "/r/.git", "/r/.git/info/exclude", WIP},
		{"rebase-merge dir", "/r", "/r/.git", "/r/.git/rebase-merge", Refs | WIP},
		{"rebase-merge file", "/r", "/r/.git", "/r/.git/rebase-merge/done", Refs | WIP},
		{"rebase-apply file", "/r", "/r/.git", "/r/.git/rebase-apply/next", Refs | WIP},
		{"sequencer file", "/r", "/r/.git", "/r/.git/sequencer/todo", Refs | WIP},
		{"rebase-merge-like name", "/r", "/r/.git", "/r/.git/rebase-merged", 0},
		{"objects dir", "/r", "/r/.git", "/r/.git/objects", 0},
		{"objects pack", "/r", "/r/.git", "/r/.git/objects/pack/x.pack", 0},
		{"worktrees admin", "/r", "/r/.git", "/r/.git/worktrees/w/HEAD", 0},
		{"root dir event", "/r", "/r/.git", "/r", WIP},
		{"subdir event", "/r", "/r/.git", "/r/src", WIP},
		{"ignored dir event", "/r", "/r/.git", "/r/build", 0},
		{"trailing slash dir", "/r", "/r/.git", "/r/src/", WIP},
		{"sibling prefix", "/r", "/r/.git", "/rr/a", 0},
		{"unclean path", "/r", "/r/.git", "/r/src/../a.txt", WIP},
		{"lock in worktree is WIP", "/r", "/r/.git", "/r/a.lock", WIP},
		{"gitignore", "/r", "/r/.git", "/r/.gitignore", WIP},
		{".git file in linked worktree", "/w", "/m/.git/worktrees/w", "/w/.git", 0},
		{"linked index", "/w", "/m/.git/worktrees/w", "/m/.git/worktrees/w/index", WIP},
		{"linked HEAD", "/w", "/m/.git/worktrees/w", "/m/.git/worktrees/w/HEAD", Refs | WIP},
		{"linked file", "/w", "/m/.git/worktrees/w", "/w/a.txt", WIP},
		{"gitDir not under root, other path", "/w", "/m/.git/worktrees/w", "/m/.git/refs/heads/x", 0},
	}
	for _, c := range cases {
		if got := Classify(c.root, c.gitDir, c.path, ign); got != c.want {
			t.Errorf("%s: Classify(%s) = %d, want %d", c.name, c.path, got, c.want)
		}
	}
}

func TestClassifyCaseInsensitive(t *testing.T) {
	old := caseInsensitive
	defer func() { caseInsensitive = old }()

	caseInsensitive = true
	if got := Classify("/Users/Me/Repo", "/Users/Me/Repo/.git", "/users/me/repo/a.txt", nil); got != WIP {
		t.Errorf("case-insensitive worktree path = %d, want WIP", got)
	}
	if got := Classify("/Users/Me/Repo", "/Users/Me/Repo/.git", "/users/me/repo/.GIT/refs/heads/x", nil); got != Refs {
		t.Errorf("case-insensitive git path = %d, want Refs", got)
	}
	ign := func(rel string) bool { return rel == "build" }
	if got := Classify("/r", "/r/.git", "/R/build", ign); got != 0 {
		t.Errorf("rel must keep the on-disk casing of the event; got %d", got)
	}

	caseInsensitive = false
	if got := Classify("/r", "/r/.git", "/R/a.txt", nil); got != 0 {
		t.Errorf("case-sensitive mismatch = %d, want 0", got)
	}
}
