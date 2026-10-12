package gitexec

import "testing"

func TestParseProgressLine(t *testing.T) {
	p, ok := ParseProgressLine("Receiving objects:  45% (450/1000), 1.20 MiB | 2.00 MiB/s")
	if !ok || p.Phase != "Receiving objects" || p.Percent != 45 || p.Current != 450 || p.Total != 1000 {
		t.Fatalf("got %+v %v", p, ok)
	}
	p, ok = ParseProgressLine("remote: Counting objects: 100% (7/7), done.")
	if !ok || p.Phase != "Counting objects" || p.Percent != 100 {
		t.Fatalf("got %+v %v", p, ok)
	}
	if _, ok := ParseProgressLine("From https://example.com/r"); ok {
		t.Fatal("non-progress line parsed")
	}
}

func TestProgressParserSplitsCR(t *testing.T) {
	var got []int
	pp := &ProgressParser{On: func(p Progress) { got = append(got, p.Percent) }}
	pp.Write([]byte("Receiving objects:  10% (1/10)\rReceiving obj"))
	pp.Write([]byte("ects:  20% (2/10)\rReceiving objects: 100% (10/10), done.\n"))
	if len(got) != 3 || got[0] != 10 || got[1] != 20 || got[2] != 100 {
		t.Fatalf("got %v", got)
	}
}
