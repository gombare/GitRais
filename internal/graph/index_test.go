package graph

import "testing"

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
