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
