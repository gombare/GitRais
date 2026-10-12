// Package graph computes the lane layout of the commit graph.
package graph

type Seg struct{ From, To, Color uint16 }

type Commit struct {
	Hash    string
	Parents []string
}

type Placement struct {
	Lane, Color int
	Up, Down    []Seg
}

type Layout struct {
	lanes  []string // expected hash per lane; "" = free
	colors []int
	next   int
}

func (l *Layout) alloc(hash string) int {
	// Colors are uint16 in Seg: the counter wraps to 0 so node and segment colors agree.
	color := l.next
	l.next = (l.next + 1) & 0xFFFF
	for i, h := range l.lanes {
		if h == "" {
			l.lanes[i], l.colors[i] = hash, color
			return i
		}
	}
	l.lanes = append(l.lanes, hash)
	l.colors = append(l.colors, color)
	return len(l.lanes) - 1
}

func (l *Layout) find(hash string) int {
	for i, h := range l.lanes {
		if h == hash {
			return i
		}
	}
	return -1
}

func seg(from, to, color int) Seg { return Seg{uint16(from), uint16(to), uint16(color)} }

func (l *Layout) Next(c Commit) Placement {
	col := l.find(c.Hash)
	isNew := col < 0
	if isNew {
		col = l.alloc(c.Hash)
	}
	up := make([]Seg, 0, len(l.lanes))
	for i, h := range l.lanes {
		if h == "" || (isNew && i == col) {
			continue
		}
		if h == c.Hash {
			up = append(up, seg(i, col, l.colors[i]))
			if i != col {
				l.lanes[i] = ""
			}
		} else {
			up = append(up, seg(i, i, l.colors[i]))
		}
	}
	color := l.colors[col]

	fresh := map[int]bool{}
	var diag []int
	if len(c.Parents) == 0 {
		l.lanes[col] = ""
	} else {
		l.lanes[col] = c.Parents[0]
		for _, p := range c.Parents[1:] {
			j := l.find(p)
			if j == col {
				continue
			}
			if j < 0 {
				j = l.alloc(p)
				fresh[j] = true
			}
			diag = append(diag, j)
		}
	}

	down := make([]Seg, 0, len(l.lanes)+len(diag))
	for j, h := range l.lanes {
		if h == "" || fresh[j] {
			continue
		}
		down = append(down, seg(j, j, l.colors[j]))
	}
	for _, j := range diag {
		down = append(down, seg(col, j, l.colors[j]))
	}

	for n := len(l.lanes); n > 0 && l.lanes[n-1] == ""; n-- {
		l.lanes, l.colors = l.lanes[:n-1], l.colors[:n-1]
	}
	return Placement{Lane: col, Color: color, Up: up, Down: down}
}

// clone returns an independent copy of the layout state.
func (l *Layout) clone() Layout {
	return Layout{lanes: append([]string(nil), l.lanes...), colors: append([]int(nil), l.colors...), next: l.next}
}
