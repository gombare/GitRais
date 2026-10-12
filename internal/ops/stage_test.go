package ops

import (
	"context"
	"os"
	"strings"
	"testing"

	"gitrais/internal/diff"
	"gitrais/internal/gitexec"
	"gitrais/internal/status"
	"gitrais/internal/testutil"
)

var bg = context.Background()

func st(t *testing.T, repo *testutil.Repo) status.Status {
	t.Helper()
	s, err := status.Read(bg, repo.R, repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStageUnstageFiles(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("one")
	repo.Write("a.txt", "2\n")
	repo.Write("new.txt", "n\n")
	if err := StageFiles(bg, repo.R, repo.Dir, []string{"a.txt", "new.txt"}); err != nil {
		t.Fatal(err)
	}
	if s := st(t, repo); len(s.Staged()) != 2 {
		t.Fatalf("staged = %+v", s.Staged())
	}
	if err := UnstageFiles(bg, repo.R, repo.Dir, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if s := st(t, repo); len(s.Staged()) != 1 || s.Staged()[0].Path != "new.txt" {
		t.Fatalf("staged = %+v", s.Staged())
	}
	if err := UnstageAll(bg, repo.R, repo.Dir); err != nil {
		t.Fatal(err)
	}
	if err := StageAll(bg, repo.R, repo.Dir); err != nil {
		t.Fatal(err)
	}
	if s := st(t, repo); len(s.Staged()) != 2 || len(s.Unstaged()) != 0 {
		t.Fatalf("after StageAll: %+v", s)
	}
}

func TestUnstageOnUnbornBranch(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	if err := StageAll(bg, repo.R, repo.Dir); err != nil {
		t.Fatal(err)
	}
	// an "AM" file (staged, then modified in the worktree) must be unstageable
	// too, and its worktree content must survive.
	repo.Write("am.txt", "staged\n")
	repo.Git("add", "am.txt")
	repo.Write("am.txt", "modified\n")
	if err := UnstageFiles(bg, repo.R, repo.Dir, []string{"am.txt"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(repo.Path("am.txt")); string(b) != "modified\n" {
		t.Fatalf("am.txt = %q", b)
	}
	repo.Git("add", "am.txt")
	repo.Write("am.txt", "modified again\n")
	if err := UnstageAll(bg, repo.R, repo.Dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(repo.Path("am.txt")); string(b) != "modified again\n" {
		t.Fatalf("am.txt = %q", b)
	}
	if s := st(t, repo); len(s.Staged()) != 0 {
		t.Fatalf("staged = %+v", s.Staged())
	}
	if err := StageAll(bg, repo.R, repo.Dir); err != nil {
		t.Fatal(err)
	}
	// staged diff on an unborn branch works
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "a.txt", Staged: true})
	if err != nil || fd.Status != "A" || len(fd.Hunks) != 1 {
		t.Fatalf("fd = %+v err = %v", fd, err)
	}
	if err := UnstageFiles(bg, repo.R, repo.Dir, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := StageAll(bg, repo.R, repo.Dir); err != nil {
		t.Fatal(err)
	}
	if err := UnstageAll(bg, repo.R, repo.Dir); err != nil {
		t.Fatal(err)
	}
	if s := st(t, repo); len(s.Staged()) != 0 {
		t.Fatalf("staged = %+v", s.Staged())
	}
}

func TestPartialStageAndUnstage(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\nB\nc\nd\ne\nf\ng\nh\ni\nJ\n")
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fd.Hunks) != 2 {
		t.Fatalf("hunks = %d", len(fd.Hunks))
	}
	p, _ := diff.BuildPatch(fd, diff.SelectHunk(fd, 0), diff.ForStage)
	if err := ApplyPatch(bg, repo.R, repo.Dir, p, false); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("diff", "--cached", "--numstat"); got != "1\t1\tf.txt" {
		t.Fatalf("staged numstat = %q", got)
	}
	staged, _ := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt", Staged: true})
	p, _ = diff.BuildPatch(staged, diff.SelectHunk(staged, 0), diff.ForUnstage)
	if err := ApplyPatch(bg, repo.R, repo.Dir, p, true); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("diff", "--cached", "--numstat"); got != "" {
		t.Fatalf("after unstage: %q", got)
	}
	if got := repo.Git("diff", "--numstat"); got != "2\t2\tf.txt" {
		t.Fatalf("worktree intact? %q", got)
	}
}

// stageLines stages the given line indices of hunk hi of the unstaged diff.
func stageLines(t *testing.T, repo *testutil.Repo, path string, hi int, lines ...int) {
	t.Helper()
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	sel := diff.Selection{hi: {}}
	for _, l := range lines {
		sel[hi][l] = true
	}
	p, ok := diff.BuildPatch(fd, sel, diff.ForStage)
	if !ok {
		t.Fatal("no patch")
	}
	if err := ApplyPatch(bg, repo.R, repo.Dir, p, false); err != nil {
		t.Fatalf("apply: %v\n%s", err, p)
	}
}

func TestPartialLineStageAndUnstageRoundTrip(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\nb\nc\nd\ne\n")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\nB\nc\nD\nX\ne\n")
	// hunk lines: 0 a,1 -b,2 +B,3 c,4 -d,5 +D,6 +X,7 e. Stage only -b/+B and +X.
	stageLines(t, repo, "f.txt", 0, 1, 2, 6)
	if got := repo.Git("show", ":f.txt"); got != "a\nB\nc\nd\nX\ne" {
		t.Fatalf("index = %q", got)
	}
	// unstage only +X again
	staged, _ := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt", Staged: true})
	var xi int
	for i, l := range staged.Hunks[0].Lines {
		if l.Kind == diff.Add && l.Text == "X" {
			xi = i
		}
	}
	p, ok := diff.BuildPatch(staged, diff.Selection{0: {xi: true}}, diff.ForUnstage)
	if !ok {
		t.Fatal("no patch")
	}
	if err := ApplyPatch(bg, repo.R, repo.Dir, p, true); err != nil {
		t.Fatalf("%v\n%s", err, p)
	}
	if got := repo.Git("show", ":f.txt"); got != "a\nB\nc\nd\ne" {
		t.Fatalf("index = %q", got)
	}
}

func TestPartialStageNoNewlineAtEOF(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\nb\nc")
	repo.CommitAll("base")
	repo.Write("f.txt", "A\nb\nC")
	fd, _ := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt"})
	// lines: 0 -a,1 +A,2 b,3 -c,4 \,5 +C,6 \ ; stage only the first change
	stageLines(t, repo, "f.txt", 0, 0, 1)
	if got := repo.Git("show", ":f.txt"); got != "A\nb\nc" {
		t.Fatalf("index = %q (diff %+v)", got, fd.Hunks[0].Lines)
	}
	// now stage the remaining change (select every changed line)
	fd, _ = WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt"})
	var rest []int
	for i, l := range fd.Hunks[0].Lines {
		if l.Kind == diff.Add || l.Kind == diff.Del {
			rest = append(rest, i)
		}
	}
	stageLines(t, repo, "f.txt", 0, rest...)
	if out, _ := os.ReadFile(repo.Path("f.txt")); string(out) != "A\nb\nC" {
		t.Fatal("worktree changed")
	}
	if got := repo.Git("diff", "--numstat"); got != "" {
		t.Fatalf("unstaged left: %q", got)
	}
}

func TestPartialStageCRLF(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\r\nb\r\nc\r\n")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\r\nB\r\nC\r\n")
	// 0 a,1 -b,2 -c,3 +B,4 +C ; stage -b +B only: unselected -c stays as context
	fd, _ := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt"})
	var sel []int
	for i, l := range fd.Hunks[0].Lines {
		if (l.Kind == diff.Del && l.Text == "b\r") || (l.Kind == diff.Add && l.Text == "B\r") {
			sel = append(sel, i)
		}
	}
	stageLines(t, repo, "f.txt", 0, sel...)
	b, err := os.ReadFile(repo.Path("f.txt"))
	if err != nil || string(b) != "a\r\nB\r\nC\r\n" {
		t.Fatal("worktree changed")
	}
	if got := indexBlob(t, repo, "f.txt"); got != "a\r\nc\r\nB\r\n" {
		t.Fatalf("index = %q", got)
	}
}

func TestPartialStageSpacesAndNonASCII(t *testing.T) {
	repo := testutil.NewRepo(t)
	for _, name := range []string{"dir with space/café ü.txt", "tab\tname.txt"} {
		repo.Write(name, "a\nb\nc\n")
		repo.CommitAll("base " + name)
		repo.Write(name, "a\nB\nc\nd\n")
		fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: name})
		if err != nil || fd.NewPath != name {
			t.Fatalf("%q: fd = %+v err = %v", name, fd, err)
		}
		p, ok := diff.BuildPatch(fd, diff.SelectHunk(fd, 0), diff.ForStage)
		if !ok {
			t.Fatal("no patch")
		}
		if err := ApplyPatch(bg, repo.R, repo.Dir, p, false); err != nil {
			t.Fatalf("%q: %v\n%s", name, err, p)
		}
		if got := repo.Git("--no-pager", "diff", "--cached", "--numstat"); !strings.HasPrefix(got, "2\t1\t") {
			t.Fatalf("%q: numstat %q", name, got)
		}
		repo.CommitAll("commit " + name)
	}
}

func TestDiffIgnoresUserPrefixConfig(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Git("config", "diff.noprefix", "true")
	repo.Git("config", "diff.mnemonicPrefix", "true")
	repo.Write("f.txt", "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\nB\nc\nd\ne\nf\ng\nh\ni\nJ\n")
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt"})
	if err != nil || fd.NewPath != "f.txt" || fd.OldPath != "f.txt" || len(fd.Hunks) != 2 {
		t.Fatalf("fd = %+v err = %v", fd, err)
	}
	p, _ := diff.BuildPatch(fd, diff.SelectHunk(fd, 1), diff.ForStage)
	if err := ApplyPatch(bg, repo.R, repo.Dir, p, false); err != nil {
		t.Fatal(err)
	}
	if got := repo.Git("diff", "--cached", "--numstat"); got != "1\t1\tf.txt" {
		t.Fatalf("numstat = %q", got)
	}
	repo.Git("commit", "-q", "-m", "second")
	cfd, err := CommitFileDiff(bg, repo.R, repo.Dir, "HEAD~1", "HEAD", "f.txt", "")
	if err != nil || cfd.NewPath != "f.txt" || len(cfd.Hunks) != 1 {
		t.Fatalf("cfd = %+v err = %v", cfd, err)
	}
}

func TestPathsAreNeverOptions(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("base.txt", "1\n")
	repo.CommitAll("base")
	repo.Write("--output=x", "hello\n")
	if err := StageFiles(bg, repo.R, repo.Dir, []string{"--output=x"}); err != nil {
		t.Fatal(err)
	}
	if s := st(t, repo); len(s.Staged()) != 1 || s.Staged()[0].Path != "--output=x" {
		t.Fatalf("staged = %+v", s.Staged())
	}
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "--output=x", Staged: true})
	if err != nil || fd.Status != "A" || fd.NewPath != "--output=x" {
		t.Fatalf("fd = %+v err = %v", fd, err)
	}
	if err := UnstageFiles(bg, repo.R, repo.Dir, []string{"--output=x"}); err != nil {
		t.Fatal(err)
	}
	fd, err = WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "--output=x", Untracked: true})
	if err != nil || fd.Status != "A" || fd.NewPath != "--output=x" {
		t.Fatalf("untracked fd = %+v err = %v", fd, err)
	}
	if b, _ := os.ReadFile(repo.Path("--output=x")); string(b) != "hello\n" {
		t.Fatalf("file clobbered: %q", b)
	}
	// a rev that looks like an option must not be interpreted as one
	if _, err := CommitFileDiff(bg, repo.R, repo.Dir, "--output=y", "HEAD", "base.txt", ""); err == nil {
		t.Fatal("expected error for bogus rev")
	}
	if _, err := os.Stat(repo.Path("y")); !os.IsNotExist(err) {
		t.Fatal("--output=y was interpreted as an option")
	}
	if err := DiscardFiles(bg, repo.R, repo.Dir, nil, []string{"--output=x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(repo.Path("--output=x")); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
}

func TestDiscard(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("a.txt", "1\n")
	repo.CommitAll("one")
	repo.Write("a.txt", "2\n")
	repo.Write("junk.txt", "x\n")
	if err := DiscardFiles(bg, repo.R, repo.Dir, []string{"a.txt"}, []string{"junk.txt"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(repo.Path("a.txt")); string(b) != "1\n" {
		t.Fatalf("a.txt = %q", b)
	}
	if _, err := os.Stat(repo.Path("junk.txt")); !os.IsNotExist(err) {
		t.Fatal("junk.txt not removed")
	}
	for _, bad := range []string{"../escape.txt", ".git/config", ".", ""} {
		if err := DiscardFiles(bg, repo.R, repo.Dir, nil, []string{bad}); err == nil {
			t.Fatalf("path %q must be rejected", bad)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(outside+"/victim", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, repo.Path("link")); err != nil {
		t.Fatal(err)
	}
	if err := DiscardFiles(bg, repo.R, repo.Dir, nil, []string{"link/victim"}); err == nil {
		t.Fatal("symlink escape must be rejected")
	}
	if _, err := os.Stat(outside + "/victim"); err != nil {
		t.Fatal("victim was deleted")
	}
}

func TestWipDiffUntracked(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.CommitAll("empty")
	repo.Write("n.txt", "x\ny\n")
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "n.txt", Untracked: true})
	if err != nil {
		t.Fatal(err)
	}
	if fd.Status != "A" || fd.NewPath != "n.txt" || len(fd.Hunks) != 1 || len(fd.Hunks[0].Lines) != 2 {
		t.Fatalf("fd = %+v", fd)
	}
}

// indexBlob returns the raw (untrimmed) bytes of the index entry for path.
func indexBlob(t *testing.T, repo *testutil.Repo, path string) string {
	t.Helper()
	out, err := repo.R.Run(bg, gitexec.Cmd{Dir: repo.Dir, Args: []string{"cat-file", "blob", ":" + path}})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func selectLines(fd diff.FileDiff, hi int, match func(diff.Line) bool) diff.Selection {
	sel := diff.Selection{hi: {}}
	for i, l := range fd.Hunks[hi].Lines {
		if match(l) {
			sel[hi][i] = true
		}
	}
	return sel
}

// A line without trailing newline that stays in the result but is followed by
// other selected lines must gain a newline; otherwise git apply glues the
// next line onto it.
func TestPartialStageAfterNoEOLLine(t *testing.T) {
	for _, eol := range []string{"\n", "\r\n"} {
		repo := testutil.NewRepo(t)
		repo.Write("f.txt", "a"+eol+"b")
		repo.CommitAll("base")
		repo.Write("f.txt", "a"+eol+"b"+eol+"c"+eol)
		fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt"})
		if err != nil {
			t.Fatal(err)
		}
		// diff: " a", "-b", "\", "+b", "+c": stage only "+c"
		sel := selectLines(fd, 0, func(l diff.Line) bool { return l.Kind == diff.Add && strings.TrimSuffix(l.Text, "\r") == "c" })
		p, ok := diff.BuildPatch(fd, sel, diff.ForStage)
		if !ok {
			t.Fatal("no patch")
		}
		if err := ApplyPatch(bg, repo.R, repo.Dir, p, false); err != nil {
			t.Fatalf("apply: %v\n%s", err, p)
		}
		if got, want := indexBlob(t, repo, "f.txt"), "a"+eol+"b"+eol+"c"+eol; got != want {
			t.Fatalf("index = %q, want %q\n%s", got, want, p)
		}
	}
}

// Unstage mirror: HEAD "a\nb", index "a\nb\nc\n", diff " a", "-b", "\", "+b",
// "+c". Unstaging only "-b" restores HEAD's "b" line while keeping the staged
// "b" and "c" lines; the restored line is no longer last, so it gets a
// newline: the index becomes "a\nb\nb\nc\n".
func TestPartialUnstageNoEOLLineFollowedByContext(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\nb")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\nb\nc\n")
	repo.Git("add", "f.txt")
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt", Staged: true})
	if err != nil {
		t.Fatal(err)
	}
	sel := selectLines(fd, 0, func(l diff.Line) bool { return l.Kind == diff.Del })
	p, ok := diff.BuildPatch(fd, sel, diff.ForUnstage)
	if !ok {
		t.Fatal("no patch")
	}
	if err := ApplyPatch(bg, repo.R, repo.Dir, p, true); err != nil {
		t.Fatalf("apply: %v\n%s", err, p)
	}
	if got := indexBlob(t, repo, "f.txt"); got != "a\nb\nb\nc\n" {
		t.Fatalf("index = %q\n%s", got, p)
	}
	if b, _ := os.ReadFile(repo.Path("f.txt")); string(b) != "a\nb\nc\n" {
		t.Fatalf("worktree changed: %q", b)
	}
}

func TestDiscardRefusesGitDirAnyCase(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("base.txt", "1\n")
	repo.CommitAll("base")
	repo.Write("sub/.git/x", "nested\n")
	for _, bad := range []string{".GIT/description", ".Git/config", "sub/.git/x", "sub/.GIT/x", "sub/.git", "./.git/description"} {
		if err := DiscardFiles(bg, repo.R, repo.Dir, nil, []string{bad}); err == nil {
			t.Fatalf("path %q must be rejected", bad)
		}
	}
	if _, err := os.Stat(repo.Path(".git/description")); err != nil {
		t.Fatalf(".git/description removed: %v", err)
	}
	if _, err := os.Stat(repo.Path("sub/.git/x")); err != nil {
		t.Fatalf("sub/.git/x removed: %v", err)
	}
}

// Only untracked files are deleted: tracked/staged and ignored files inside an
// "untracked" directory survive.
func TestDiscardUntrackedKeepsTrackedAndIgnored(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write(".gitignore", "*.log\n")
	repo.CommitAll("base")
	repo.Write("d/staged.txt", "s\n")
	repo.Git("add", "d/staged.txt")
	repo.Write("d/junk.txt", "j\n")
	repo.Write("d/keep.log", "l\n")
	repo.Write("d/e/deep.txt", "x\n")
	if err := DiscardFiles(bg, repo.R, repo.Dir, nil, []string{"d"}); err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"d/staged.txt", "d/keep.log"} {
		if _, err := os.Stat(repo.Path(keep)); err != nil {
			t.Fatalf("%s removed: %v", keep, err)
		}
	}
	for _, gone := range []string{"d/junk.txt", "d/e"} {
		if _, err := os.Stat(repo.Path(gone)); !os.IsNotExist(err) {
			t.Fatalf("%s not removed", gone)
		}
	}
}

func TestWipDiffIgnoresTextconv(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Git("config", "diff.up.textconv", "tr a-z A-Z <")
	repo.Write(".gitattributes", "*.txt diff=up\n")
	repo.Write("f.txt", "a\nb\nc\n")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\nx\nc\n")
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt"})
	if err != nil || len(fd.Hunks) != 1 {
		t.Fatalf("fd = %+v err = %v", fd, err)
	}
	p, ok := diff.BuildPatch(fd, diff.SelectHunk(fd, 0), diff.ForStage)
	if !ok {
		t.Fatal("no patch")
	}
	if err := ApplyPatch(bg, repo.R, repo.Dir, p, false); err != nil {
		t.Fatalf("apply: %v\n%s", err, p)
	}
	if got := indexBlob(t, repo, "f.txt"); got != "a\nx\nc\n" {
		t.Fatalf("index = %q", got)
	}
	cfd, err := CommitFileDiff(bg, repo.R, repo.Dir, "HEAD", "HEAD", "f.txt", "")
	_ = cfd
	if err != nil {
		t.Fatal(err)
	}
}

func TestWipDiffUnmergedIsConflicted(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "1\n2\n3\n")
	repo.CommitAll("base")
	repo.Git("checkout", "-q", "-b", "side")
	repo.Write("f.txt", "1\nS\n3\n")
	repo.CommitAll("side")
	repo.Git("checkout", "-q", "-")
	repo.Write("f.txt", "1\nM\n3\n")
	repo.CommitAll("main")
	if _, err := repo.R.Run(bg, gitexec.Cmd{Dir: repo.Dir, Args: []string{"merge", "side"}}); err == nil {
		t.Fatal("expected conflict")
	}
	for _, staged := range []bool{false, true} {
		fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt", Staged: staged})
		if err != nil {
			t.Fatal(err)
		}
		if fd.Status != "U" {
			t.Fatalf("staged=%v: status = %q, want U", staged, fd.Status)
		}
	}
}

func TestPartialStageAfterBareCRLine(t *testing.T) {
	repo := testutil.NewRepo(t)
	repo.Write("f.txt", "a\nb\r")
	repo.CommitAll("base")
	repo.Write("f.txt", "a\nb\r\nc\n")
	fd, err := WipDiff(bg, repo.R, repo.Dir, WipDiffRequest{Path: "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	sel := selectLines(fd, 0, func(l diff.Line) bool { return l.Kind == diff.Add && l.Text == "c" })
	p, ok := diff.BuildPatch(fd, sel, diff.ForStage)
	if !ok {
		t.Fatal("no patch")
	}
	if err := ApplyPatch(bg, repo.R, repo.Dir, p, false); err != nil {
		t.Fatalf("apply: %v\n%s", err, p)
	}
	if got, want := indexBlob(t, repo, "f.txt"), "a\nb\r\nc\n"; got != want {
		t.Fatalf("index = %q, want %q\n%s", got, want, p)
	}
}
