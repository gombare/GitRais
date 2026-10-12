package diff

import "testing"

const multi = `diff --git a/a.txt b/a.txt
index 1111111..2222222 100644
--- a/a.txt
+++ b/a.txt
@@ -1,3 +1,4 @@ func x
 one
-two
+TWO
+2.5
 three
@@ -10 +11 @@
-old
\ No newline at end of file
+new
\ No newline at end of file
diff --git a/old name.txt b/new name.txt
similarity index 90%
rename from old name.txt
rename to new name.txt
index 3333333..4444444 100644
--- a/old name.txt
+++ b/new name.txt
@@ -1 +1 @@
---dashes
+++plus
diff --git a/new.bin b/new.bin
new file mode 100644
index 0000000..5555555
Binary files /dev/null and b/new.bin differ
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index 6666666..0000000
--- a/gone.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
diff --git "a/caf\303\251.txt" "b/caf\303\251.txt"
old mode 100644
new mode 100755
`

func TestParse(t *testing.T) {
	files, err := Parse([]byte(multi))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("files = %d", len(files))
	}
	a := files[0]
	if a.NewPath != "a.txt" || a.Status != "M" || len(a.Hunks) != 2 {
		t.Fatalf("a = %+v", a)
	}
	h := a.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 3 || h.NewStart != 1 || h.NewLines != 4 || h.Section != "func x" || len(h.Lines) != 5 {
		t.Fatalf("hunk0 = %+v", h)
	}
	if l := h.Lines[3]; l.Kind != Add || l.Text != "2.5" || l.NewNo != 3 || l.OldNo != 0 {
		t.Fatalf("line = %+v", l)
	}
	if l := h.Lines[4]; l.Kind != Context || l.OldNo != 3 || l.NewNo != 4 {
		t.Fatalf("ctx = %+v", l)
	}
	h2 := a.Hunks[1]
	if h2.OldLines != 1 || h2.NewLines != 1 || len(h2.Lines) != 4 || h2.Lines[1].Kind != NoNewline {
		t.Fatalf("hunk1 = %+v", h2)
	}
	r := files[1]
	if r.Status != "R" || r.OldPath != "old name.txt" || r.NewPath != "new name.txt" || r.Similarity != 90 {
		t.Fatalf("rename = %+v", r)
	}
	if l := r.Hunks[0].Lines; len(l) != 2 || l[0].Text != "--dashes" || l[1].Text != "++plus" {
		t.Fatalf("dash lines = %+v", l)
	}
	if b := files[2]; !b.Binary || b.Status != "A" || b.NewPath != "new.bin" {
		t.Fatalf("binary = %+v", b)
	}
	if d := files[3]; d.Status != "D" || d.OldPath != "gone.txt" || len(d.Hunks) != 1 {
		t.Fatalf("delete = %+v", d)
	}
	if m := files[4]; m.NewPath != "café.txt" || m.OldMode != "100644" || m.NewMode != "100755" {
		t.Fatalf("mode = %+v", m)
	}
}

func TestParseEmpty(t *testing.T) {
	for _, in := range []string{"", "\n"} {
		files, err := Parse([]byte(in))
		if err != nil || files == nil || len(files) != 0 {
			t.Fatalf("Parse(%q) = %v, %v", in, files, err)
		}
	}
}

func TestParseCRLFKeepsCR(t *testing.T) {
	in := "diff --git a/w.txt b/w.txt\n--- a/w.txt\n+++ b/w.txt\n@@ -1,2 +1,2 @@\n a\r\n-b\r\n+c\r\n"
	files, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	l := files[0].Hunks[0].Lines
	if len(l) != 3 || l[0].Text != "a\r" || l[1].Text != "b\r" || l[2].Text != "c\r" {
		t.Fatalf("lines = %+v", l)
	}
	if l[1].OldNo != 2 || l[2].NewNo != 2 {
		t.Fatalf("numbers = %+v", l)
	}
}

func TestParsePureRenameAndTabSuffixedHeaders(t *testing.T) {
	in := "diff --git a/x y.txt b/z w.txt\nsimilarity index 100%\nrename from x y.txt\nrename to z w.txt\n" +
		"diff --git a/sp ace.txt b/sp ace.txt\n--- a/sp ace.txt\t\n+++ b/sp ace.txt\t\n@@ -1 +1 @@\n-a\n+b\n" +
		"diff --git \"a/q\\\"t.txt\" \"b/q\\\"t.txt\"\n--- \"a/q\\\"t.txt\"\n+++ \"b/q\\\"t.txt\"\n@@ -0,0 +1 @@\n+n\n"
	files, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %+v", files)
	}
	if f := files[0]; f.Status != "R" || f.OldPath != "x y.txt" || f.NewPath != "z w.txt" || f.Similarity != 100 || len(f.Hunks) != 0 {
		t.Fatalf("rename = %+v", f)
	}
	if f := files[1]; f.OldPath != "sp ace.txt" || f.NewPath != "sp ace.txt" || len(f.Hunks) != 1 {
		t.Fatalf("space = %+v", f)
	}
	if f := files[2]; f.NewPath != `q"t.txt` || f.OldPath != `q"t.txt` {
		t.Fatalf("quoted = %+v", f)
	}
}

func TestParseNewEmptyFileAndInvalidHunk(t *testing.T) {
	files, err := Parse([]byte("diff --git a/e b/e\nnew file mode 100644\nindex 0000000..e69de29\n"))
	if err != nil || len(files) != 1 || files[0].Status != "A" || files[0].NewPath != "e" || len(files[0].Hunks) != 0 {
		t.Fatalf("empty new = %+v, %v", files, err)
	}
	if _, err := Parse([]byte("diff --git a/e b/e\n@@ bogus @@\n")); err == nil {
		t.Fatal("expected error for invalid hunk header")
	}
}
