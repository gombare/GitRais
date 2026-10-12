package graph

import (
	"reflect"
	"testing"
)

type want struct {
	lane     int
	up, down []Seg
}

func run(t *testing.T, commits []Commit, wants []want) {
	t.Helper()
	var l Layout
	for i, c := range commits {
		p := l.Next(c)
		w := wants[i]
		strip := func(s []Seg) []Seg { // i test confrontano solo la geometria
			out := []Seg{}
			for _, x := range s {
				out = append(out, Seg{From: x.From, To: x.To})
			}
			return out
		}
		if p.Lane != w.lane || !reflect.DeepEqual(strip(p.Up), nz(w.up)) || !reflect.DeepEqual(strip(p.Down), nz(w.down)) {
			t.Fatalf("row %d (%s): got lane=%d up=%v down=%v; want lane=%d up=%v down=%v",
				i, c.Hash, p.Lane, strip(p.Up), strip(p.Down), w.lane, w.up, w.down)
		}
	}
}

func nz(s []Seg) []Seg {
	if s == nil {
		return []Seg{}
	}
	return s
}

func TestLinear(t *testing.T) {
	run(t, []Commit{{"C", []string{"B"}}, {"B", []string{"A"}}, {"A", nil}}, []want{
		{0, nil, []Seg{{0, 0, 0}}},
		{0, []Seg{{0, 0, 0}}, []Seg{{0, 0, 0}}},
		{0, []Seg{{0, 0, 0}}, nil},
	})
}

func TestMerge(t *testing.T) {
	// M = merge(P1, P2); P2 -> P1; P1 root
	run(t, []Commit{{"M", []string{"P1", "P2"}}, {"P2", []string{"P1"}}, {"P1", nil}}, []want{
		{0, nil, []Seg{{0, 0, 0}, {0, 1, 0}}},
		{1, []Seg{{0, 0, 0}, {1, 1, 0}}, []Seg{{0, 0, 0}, {1, 1, 0}}},
		{0, []Seg{{0, 0, 0}, {1, 0, 0}}, nil},
	})
}

func TestTwoTipsShareParent(t *testing.T) {
	run(t, []Commit{{"X", []string{"B"}}, {"Y", []string{"B"}}, {"B", nil}}, []want{
		{0, nil, []Seg{{0, 0, 0}}},
		{1, []Seg{{0, 0, 0}}, []Seg{{0, 0, 0}, {1, 1, 0}}},
		{0, []Seg{{0, 0, 0}, {1, 0, 0}}, nil},
	})
}

func TestOctopus(t *testing.T) {
	run(t, []Commit{{"O", []string{"A", "B", "C"}}, {"C", []string{"A"}}, {"B", []string{"A"}}, {"A", nil}}, []want{
		{0, nil, []Seg{{0, 0, 0}, {0, 1, 0}, {0, 2, 0}}},
		{2, []Seg{{0, 0, 0}, {1, 1, 0}, {2, 2, 0}}, []Seg{{0, 0, 0}, {1, 1, 0}, {2, 2, 0}}},
		{1, []Seg{{0, 0, 0}, {1, 1, 0}, {2, 2, 0}}, []Seg{{0, 0, 0}, {1, 1, 0}, {2, 2, 0}}},
		{0, []Seg{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}}, nil},
	})
}

func TestMergeIntoExistingLane(t *testing.T) {
	// T2 e M condividono il parent P; M ha secondo parent P già atteso dalla corsia di T2.
	run(t, []Commit{{"T2", []string{"P"}}, {"M", []string{"Q", "P"}}, {"Q", []string{"P"}}, {"P", nil}}, []want{
		{0, nil, []Seg{{0, 0, 0}}},
		{1, []Seg{{0, 0, 0}}, []Seg{{0, 0, 0}, {1, 1, 0}, {1, 0, 0}}},
		{1, []Seg{{0, 0, 0}, {1, 1, 0}}, []Seg{{0, 0, 0}, {1, 1, 0}}},
		{0, []Seg{{0, 0, 0}, {1, 0, 0}}, nil},
	})
}

func TestLaneReuseAndColors(t *testing.T) {
	var l Layout
	a := l.Next(Commit{"A", []string{"R"}})
	b := l.Next(Commit{"B", nil}) // tip senza parent: occupa corsia 1 e la libera
	c := l.Next(Commit{"C", []string{"R"}})
	if b.Lane != 1 || c.Lane != 1 {
		t.Fatalf("lanes b=%d c=%d, want 1 and 1 (riuso)", b.Lane, c.Lane)
	}
	if a.Color == b.Color || b.Color == c.Color {
		t.Fatalf("colori non distinti: a=%d b=%d c=%d", a.Color, b.Color, c.Color)
	}
}
