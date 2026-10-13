package gitexec

import (
	"strings"
	"testing"
)

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

func TestNewGitErrorRedactsStderr(t *testing.T) {
	e := newGitError([]string{"fetch"}, 128, "fatal: unable to access 'https://u:secret@h/r/': boom\n", nil)
	if strings.Contains(e.Stderr, "secret") || strings.Contains(e.Error(), "secret") {
		t.Fatalf("credentials leaked: %q", e.Stderr)
	}
}

func TestGitErrorMessageSkipsProgressNoise(t *testing.T) {
	cases := []struct {
		name, stderr, want string
	}{
		{"push rejected",
			"Enumerating objects: 5, done.\nCounting objects: 100% (5/5), done.\nWriting objects: 100% (3/3), 250 bytes | 250.00 KiB/s, done.\nTotal 3 (delta 0), reused 0 (delta 0), pack-reused 0\nremote: Enumerating objects: 5, done.\nremote: Counting objects: 100% (5/5), done.\nTo /tmp/bare\n ! [remote rejected] main -> main (pre-receive hook declined)\nerror: failed to push some refs to '/tmp/bare'\n",
			"! [remote rejected] main -> main (pre-receive hook declined)"},
		{"push error line",
			"remote: Enumerating objects: 5, done.\nremote: Counting objects: 100% (5/5)\rremote: Counting objects: 100% (5/5), done.\nerror: failed to push some refs to 'x'\n",
			"error: failed to push some refs to 'x'"},
		{"clone fatal",
			"Cloning into 'x'...\nremote: Enumerating objects: 3, done.\nfatal: early EOF\n",
			"fatal: early EOF"},
		{"carriage return noise only",
			"Receiving objects:  50% (1/2)\rReceiving objects: 100% (2/2)\nsomething odd happened\n",
			"something odd happened"},
		{"fallback to first non-empty",
			"\n\nplain message\nsecond\n",
			"plain message"},
	}
	for _, c := range cases {
		e := newGitError([]string{"push"}, 1, c.stderr, nil)
		want := "git push: exit 1: " + c.want
		if got := e.Error(); got != want {
			t.Errorf("%s: Error() = %q, want %q", c.name, got, want)
		}
		if e.Stderr != c.stderr {
			t.Errorf("%s: Stderr must stay complete", c.name)
		}
	}
}

func TestClassifyStaleInfoAndPullRebaseDirty(t *testing.T) {
	cases := []struct {
		stderr string
		want   Kind
	}{
		{" ! [rejected]        main -> main (stale info)\nerror: failed to push some refs", KindNonFastForward},
		{"error: cannot pull with rebase: You have unstaged changes.", KindDirtyWorktree},
		{"error: cannot pull with rebase: Your index contains uncommitted changes.", KindDirtyWorktree},
	}
	for _, c := range cases {
		if got := Classify(c.stderr); got != c.want {
			t.Errorf("Classify(%q) = %s, want %s", c.stderr, got, c.want)
		}
	}
}

func TestSummarizeSplitsOnCR(t *testing.T) {
	cases := map[string]string{
		"Receiving objects:  45% (9/20)\rfatal: the remote end hung up unexpectedly\n":                                     "fatal: the remote end hung up unexpectedly",
		"Writing objects:  50% (1/2)\rremote: fatal: pack exceeds maximum allowed size\nerror: failed to push some refs\n": "remote: fatal: pack exceeds maximum allowed size",
	}
	for in, want := range cases {
		if got := summarize(in); got != want {
			t.Errorf("summarize(%q) = %q, want %q", in, got, want)
		}
	}
}

// Untracked files in the way are not "local changes": autostash cannot help,
// so they get their own kind, checked before DirtyWorktree.
func TestClassifyUntrackedOverwrite(t *testing.T) {
	cases := []struct {
		stderr string
		want   Kind
	}{
		{"error: The following untracked working tree files would be overwritten by merge:\n\tb.txt\nPlease move or remove them before you merge.\nAborting", KindUntrackedOverwrite},
		{"error: The following untracked working tree files would be overwritten by checkout:\n\tb.txt", KindUntrackedOverwrite},
		{"error: The following untracked working tree files would be removed by merge:\n\tb.txt", KindUntrackedOverwrite},
		{"error: Your local changes to the following files would be overwritten by merge:\n\ta.txt\nPlease commit your changes or stash them before you merge.", KindDirtyWorktree},
	}
	for _, c := range cases {
		if got := Classify(c.stderr); got != c.want {
			t.Errorf("Classify(%q) = %s, want %s", c.stderr, got, c.want)
		}
	}
	if KindUntrackedOverwrite != "UntrackedWouldBeOverwritten" {
		t.Fatalf("kind = %s", KindUntrackedOverwrite)
	}
}
