package gitexec

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		stderr string
		want   Kind
	}{
		{"fatal: not a git repository (or any of the parent directories): .git", KindNotARepo},
		{"fatal: Authentication failed for 'https://example.com/r.git/'", KindAuthFailed},
		{"fatal: could not read Username for 'https://example.com': terminal prompts disabled", KindAuthFailed},
		{"git@example.com: Permission denied (publickey).", KindAuthFailed},
		{" ! [rejected]        main -> main (fetch first)\nerror: failed to push some refs", KindNonFastForward},
		{" ! [rejected]        main -> main (non-fast-forward)", KindNonFastForward},
		{"CONFLICT (content): Merge conflict in a.txt\nAutomatic merge failed", KindConflict},
		{"error: could not apply 1234abc... msg", KindConflict},
		{"error: Your local changes to the following files would be overwritten by checkout:\n\ta.txt", KindDirtyWorktree},
		{"fatal: Unable to create '/r/.git/index.lock': File exists.", KindLocked},
		{"fatal: unable to access 'https://x/': Could not resolve host: x", KindNetwork},
		{"ssh: connect to host x port 22: Connection refused", KindNetwork},
		{" ! [remote rejected] main -> main (pre-receive hook declined)", KindHookRejected},
		{"something else", KindUnknown},
	}
	for _, c := range cases {
		if got := Classify(c.stderr); got != c.want {
			t.Errorf("Classify(%q) = %s, want %s", c.stderr, got, c.want)
		}
	}
}

func TestGitErrorMessage(t *testing.T) {
	e := newGitError([]string{"push", "https://u:p@h/r.git"}, 1, "error: boom\nmore", nil)
	if e.Args[1] != "https://h/r.git" {
		t.Fatalf("args not redacted: %v", e.Args)
	}
	if got := e.Error(); got != "git push: exit 1: error: boom" {
		t.Fatalf("Error() = %q", got)
	}
}
