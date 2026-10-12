package graph

import "sync"

type RowKind uint8

const (
	RowCommit RowKind = iota
	RowStash
)

type Row struct {
	Hash     string
	Parents  []string
	Kind     RowKind
	Lane     int
	Color    int
	Up, Down []Seg
	Author   string
	Email    string
	Time     int64
	Subject  string
	StashRef string // "stash@{N}" when Kind == RowStash
}

type Index struct {
	mu     sync.RWMutex
	rows   []Row
	byHash map[string]int32
	done   bool
	err    error
}

func NewIndex() *Index { return &Index{byHash: make(map[string]int32)} }

func (x *Index) append(rows []Row) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, r := range rows {
		x.byHash[r.Hash] = int32(len(x.rows))
		x.rows = append(x.rows, r)
	}
}

func (x *Index) finish(err error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.done, x.err = true, err
}

func (x *Index) Len() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.rows)
}

func (x *Index) Done() bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.done
}

func (x *Index) Err() error {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.err
}

// Rows returns a copy of [start, start+count) clamped to the available rows.
func (x *Index) Rows(start, count int) []Row {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if start < 0 {
		start = 0
	}
	if start >= len(x.rows) || count <= 0 {
		return []Row{}
	}
	end := min(start+count, len(x.rows))
	out := make([]Row, end-start)
	copy(out, x.rows[start:end])
	return out
}

func (x *Index) Find(hash string) (int, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	i, ok := x.byHash[hash]
	return int(i), ok
}
