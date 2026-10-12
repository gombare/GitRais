package graph

import "testing"

func TestColorWrapsLikeSegments(t *testing.T) {
	l := Layout{next: 1<<16 - 1}
	l.Next(Commit{Hash: "a", Parents: []string{"b", "c"}})
	p := l.Next(Commit{Hash: "c", Parents: nil})
	if p.Color != int(p.Up[len(p.Up)-1].Color) {
		t.Fatalf("node color %d != segment color %d", p.Color, p.Up[len(p.Up)-1].Color)
	}
}
