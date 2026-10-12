package genrepo

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

func TestValidate(t *testing.T) {
	for _, c := range []struct {
		n, k int
		ok   bool
	}{{1, 1, true}, {10, 12, true}, {0, 12, false}, {-1, 12, false}, {10, 0, false}, {10, 1001, false}} {
		if err := Validate(c.n, c.k); (err == nil) != c.ok {
			t.Errorf("Validate(%d,%d) = %v, want ok=%v", c.n, c.k, err, c.ok)
		}
	}
	if err := Write(&bytes.Buffer{}, 0, 1); err == nil {
		t.Error("Write accepted n=0")
	}
}

func TestWriteDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	if err := Write(&a, 200, 5); err != nil {
		t.Fatal(err)
	}
	if err := Write(&b, 200, 5); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("output is not deterministic")
	}
	if !strings.Contains(a.String(), "\nmerge :") {
		t.Fatal("no merges generated")
	}
}

func TestCreate(t *testing.T) {
	r := testutil.Runner(t)
	dir := filepath.Join(t.TempDir(), "repo")
	for _, k := range []int{1, 7} {
		d := dir + string(rune('0'+k))
		if err := Create(context.Background(), r, d, 300, k); err != nil {
			t.Fatal(err)
		}
		git := func(args ...string) string {
			out, err := r.Run(context.Background(), gitexec.Cmd{Dir: d, Args: args})
			if err != nil {
				t.Fatal(err)
			}
			return strings.TrimSpace(string(out))
		}
		if got := git("rev-list", "--all", "--count"); got != "300" {
			t.Fatalf("k=%d: %s commits, want 300", k, got)
		}
		if got := git("symbolic-ref", "HEAD"); got != "refs/heads/main" {
			t.Fatalf("HEAD = %s", got)
		}
		merges := git("rev-list", "--all", "--merges", "--count")
		if k > 1 && merges == "0" {
			t.Fatalf("k=%d: no merges", k)
		}
		if k == 1 && merges != "0" {
			t.Fatalf("k=1: %s merges", merges)
		}
	}
}
