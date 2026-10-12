package graph

import (
	"reflect"
	"strings"
	"testing"
)

func TestIndexRowsBounds(t *testing.T) {
	x := NewIndex()
	x.append([]Row{{Hash: "a"}, {Hash: "b"}, {Hash: "c"}})
	if got := x.Rows(1, 10); len(got) != 2 || got[0].Hash != "b" {
		t.Fatalf("Rows(1,10) = %v", got)
	}
	if got := x.Rows(5, 2); len(got) != 0 {
		t.Fatalf("Rows(5,2) = %v", got)
	}
	if i, ok := x.Find("c"); !ok || i != 2 {
		t.Fatalf("Find(c) = %d %v", i, ok)
	}
	if x.Done() {
		t.Fatal("done too early")
	}
	x.finish(nil)
	if !x.Done() || x.Len() != 3 {
		t.Fatal("finish")
	}
}

func TestIndexIDFormats(t *testing.T) {
	for _, w := range []int{40, 64} {
		h := func(c byte) string { return strings.Repeat(string(c), w) }
		x := NewIndex()
		x.append([]Row{{Hash: h('a'), Parents: []string{h('b'), h('c')}, Subject: "s", Author: "A"}, {Hash: h('b')}})
		got := x.Rows(0, 2)
		if got[0].Hash != h('a') || !reflect.DeepEqual(got[0].Parents, []string{h('b'), h('c')}) ||
			got[0].Subject != "s" || got[0].Author != "A" || got[1].Hash != h('b') || len(got[1].Parents) != 0 {
			t.Fatalf("w=%d: %+v", w, got)
		}
		for _, miss := range []string{h('c'), h('a')[:w-2], strings.ToUpper(h('a')), "a"} {
			if _, ok := x.Find(miss); ok {
				t.Fatalf("w=%d: Find(%q) found", w, miss)
			}
		}
		if i, ok := x.Find(h('b')); !ok || i != 1 {
			t.Fatalf("w=%d: Find = %d %v", w, i, ok)
		}
	}
	if _, ok := NewIndex().Find("a"); ok {
		t.Fatal("Find on empty index")
	}
}

func TestIndexRejectsMixedIDFormats(t *testing.T) {
	sha1 := strings.Repeat("a", 40)
	x := NewIndex()
	if err := x.append([]Row{{Hash: sha1}}); err != nil {
		t.Fatal(err)
	}
	if err := x.append([]Row{{Hash: strings.Repeat("b", 40), Parents: []string{"zz"}}}); err == nil {
		t.Fatal("bad parent accepted")
	}
	if err := x.append([]Row{{Hash: strings.Repeat("c", 64)}}); err == nil {
		t.Fatal("SHA-256 id accepted in a SHA-1 index")
	}
	if err := x.append([]Row{{Hash: strings.Repeat("d", 40)}}); err != nil {
		t.Fatal(err)
	}
	if got := x.Rows(0, 10); x.Len() != 2 || len(got) != 2 || got[1].Hash != strings.Repeat("d", 40) || len(got[1].Parents) != 0 {
		t.Fatalf("after rejected rows: len=%d rows=%+v", x.Len(), got)
	}
	if i, ok := x.Find(strings.Repeat("d", 40)); !ok || i != 1 {
		t.Fatalf("Find = %d %v", i, ok)
	}
}
