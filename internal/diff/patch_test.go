package diff

import "testing"

const src = `diff --git a/f.txt b/f.txt
--- a/f.txt
+++ b/f.txt
@@ -1,4 +1,4 @@
 a
-b
+B
 c
-d
+D
@@ -10,2 +10,3 @@
 x
+y
 z
`

func TestBuildPatchStageSubset(t *testing.T) {
	files, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// select only "-b" and "+B" (lines 1 and 2 of the first hunk)
	p, ok := BuildPatch(files[0], Selection{0: {1: true, 2: true}}, ForStage)
	if !ok {
		t.Fatal("expected patch")
	}
	want := `diff --git a/f.txt b/f.txt
--- a/f.txt
+++ b/f.txt
@@ -1,4 +1,4 @@
 a
-b
+B
 c
 d
`
	if string(p) != want {
		t.Fatalf("patch:\n%s\nwant:\n%s", p, want)
	}
}

func TestBuildPatchOffsetAcrossHunks(t *testing.T) {
	files, _ := Parse([]byte(src))
	// staging "+D" without "-d" shifts the next hunk's NewStart by +1
	sel := Selection{0: {5: true}, 1: {1: true}}
	p, _ := BuildPatch(files[0], sel, ForStage)
	want := `diff --git a/f.txt b/f.txt
--- a/f.txt
+++ b/f.txt
@@ -1,4 +1,5 @@
 a
 b
 c
 d
+D
@@ -10,2 +11,3 @@
 x
+y
 z
`
	if string(p) != want {
		t.Fatalf("patch:\n%s\nwant:\n%s", p, want)
	}
}

func TestBuildPatchUnstage(t *testing.T) {
	files, _ := Parse([]byte(src))
	p, _ := BuildPatch(files[0], Selection{1: {1: true}}, ForUnstage)
	want := `diff --git a/f.txt b/f.txt
--- a/f.txt
+++ b/f.txt
@@ -10,2 +10,3 @@
 x
+y
 z
`
	if string(p) != want {
		t.Fatalf("patch:\n%s\nwant:\n%s", p, want)
	}
	if _, ok := BuildPatch(files[0], Selection{}, ForStage); ok {
		t.Fatal("an empty selection must return false")
	}
}

func TestSelectHunk(t *testing.T) {
	files, _ := Parse([]byte(src))
	s := SelectHunk(files[0], 0)
	if len(s[0]) != 4 || !s[0][1] || !s[0][2] || !s[0][4] || !s[0][5] {
		t.Fatalf("sel = %v", s)
	}
}

func TestBuildPatchNoNewlineDropped(t *testing.T) {
	const nn = "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n\\ No newline at end of file\n"
	files, _ := Parse([]byte(nn))
	// only "-b" selected: "+c" is dropped together with its marker
	p, ok := BuildPatch(files[0], Selection{0: {1: true}}, ForStage)
	if !ok {
		t.Fatal("expected patch")
	}
	want := "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,1 @@\n a\n-b\n"
	if string(p) != want {
		t.Fatalf("patch:\n%q\nwant:\n%q", p, want)
	}
	// both selected: the marker is kept
	p, _ = BuildPatch(files[0], Selection{0: {1: true, 2: true}}, ForStage)
	want = nn
	if string(p) != want {
		t.Fatalf("patch:\n%q\nwant:\n%q", p, want)
	}
}

func TestBuildPatchQuotesPath(t *testing.T) {
	const q = "diff --git \"a/t\\ta.txt\" \"b/t\\ta.txt\"\n--- \"a/t\\ta.txt\"\n+++ \"b/t\\ta.txt\"\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	files, _ := Parse([]byte(q))
	p, ok := BuildPatch(files[0], SelectHunk(files[0], 0), ForStage)
	if !ok || string(p) != q {
		t.Fatalf("patch:\n%q\nwant:\n%q", p, q)
	}
}

func TestBuildPatchRejectsNonModify(t *testing.T) {
	const n = "diff --git a/n b/n\nnew file mode 100644\n--- /dev/null\n+++ b/n\n@@ -0,0 +1 @@\n+x\n"
	files, _ := Parse([]byte(n))
	if _, ok := BuildPatch(files[0], SelectHunk(files[0], 0), ForStage); ok {
		t.Fatal("partial staging of added files must be refused")
	}
}

// A "\ No newline" line that stays as context but is followed by other emitted
// lines becomes -x / \ / +x, so the newline fix is staged with the selection.
func TestBuildPatchNoNewlineFollowedByEmittedLines(t *testing.T) {
	const stage = "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,3 @@\n a\n-b\n\\ No newline at end of file\n+b\n+c\n"
	files, _ := Parse([]byte(stage))
	// stage only "+c" (line 4)
	p, ok := BuildPatch(files[0], Selection{0: {4: true}}, ForStage)
	want := "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,3 @@\n a\n-b\n\\ No newline at end of file\n+b\n+c\n"
	if !ok || string(p) != want {
		t.Fatalf("stage patch:\n%q\nwant:\n%q", p, want)
	}
	// nothing emitted after the no-EOL line: it stays context with its marker
	const tail = "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n-a\n+A\n b\n\\ No newline at end of file\n"
	files, _ = Parse([]byte(tail))
	p, ok = BuildPatch(files[0], Selection{0: {0: true, 1: true}}, ForStage)
	if !ok || string(p) != tail {
		t.Fatalf("tail patch:\n%q\nwant:\n%q", p, tail)
	}
	// unstage only "-b" (line 1): the restored "b" is followed by context on
	// the result (old) side, so it loses its marker.
	files, _ = Parse([]byte(stage))
	p, ok = BuildPatch(files[0], Selection{0: {1: true}}, ForUnstage)
	want = "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,4 +1,3 @@\n a\n-b\n b\n c\n"
	if !ok || string(p) != want {
		t.Fatalf("unstage patch:\n%q\nwant:\n%q", p, want)
	}
	// both sides without EOL, only the new side selected
	const both = "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n"
	files, _ = Parse([]byte(both))
	p, ok = BuildPatch(files[0], Selection{0: {3: true}}, ForStage)
	want = "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,3 @@\n a\n-b\n\\ No newline at end of file\n+b\n+c\n\\ No newline at end of file\n"
	if !ok || string(p) != want {
		t.Fatalf("both patch:\n%q\nwant:\n%q", p, want)
	}
	// CRLF: the re-added line keeps the file's line ending
	const crlf = "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,3 @@\n a\r\n-b\n\\ No newline at end of file\n+b\r\n+c\r\n"
	files, _ = Parse([]byte(crlf))
	p, ok = BuildPatch(files[0], Selection{0: {4: true}}, ForStage)
	want = "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,3 @@\n a\r\n-b\n\\ No newline at end of file\n+b\r\n+c\r\n"
	if !ok || string(p) != want {
		t.Fatalf("crlf patch:\n%q\nwant:\n%q", p, want)
	}
}
