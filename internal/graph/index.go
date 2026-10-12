package graph

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"hash/maphash"
	"strings"
	"sync"
)

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

// The Index stores rows compactly (about 100 bytes per commit instead of
// several hundred plus one segment per active lane): object ids are binary,
// people are interned, subjects share one buffer per chunk, and the Up/Down
// segments are not stored but recomputed by Rows replaying the layout from the
// nearest checkpoint (one every checkpointEvery rows). Rows live in fixed-size
// chunks so that growing the index never copies the rows already loaded while
// holding the write lock.
const (
	checkpointEvery = 512
	chunkBits       = 12
	chunkSize       = 1 << chunkBits
)

// Object ids are stored with a fixed width per index, chosen by the first row:
// 20 or 32 bytes for SHA-1 or SHA-256 lowercase hex ids, else rawIDWidth bytes
// (a length byte and the string itself, for test fixtures).
const rawIDWidth = 65

func idWidth(s string) int {
	if (len(s) == 40 || len(s) == 64) && isLowerHex(s) {
		return len(s) / 2
	}
	return rawIDWidth
}

// encodeID writes s into dst (len(dst) is the index's id width); false if s
// does not have the index's format.
func encodeID(dst []byte, s string) bool {
	if len(dst) == rawIDWidth {
		if len(s) >= rawIDWidth {
			return false
		}
		clear(dst)
		dst[0] = byte(len(s))
		copy(dst[1:], s)
		return true
	}
	if len(s) != 2*len(dst) || !isLowerHex(s) {
		return false
	}
	_, err := hex.Decode(dst, []byte(s))
	return err == nil
}

func decodeID(b []byte) string {
	if len(b) == rawIDWidth {
		return string(b[1 : 1+b[0]])
	}
	return hex.EncodeToString(b)
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// node is the stored form of a Row without its hash, parents and segments.
type node struct {
	time    int64
	subject uint32 // offset in chunk.text
	sublen  uint32
	par     uint32 // first parent id in chunk.parents
	npar    uint32
	author  uint32 // index into Index.people
	email   uint32
	lane    int32
	color   uint16 // Layout colors wrap at 0xFFFF
	kind    RowKind
}

type chunk struct {
	nodes   []node // len <= chunkSize
	ids     []byte // id of nodes[i] at [i*w, (i+1)*w)
	parents []byte // parent ids, w bytes each
	text    []byte // subjects
	// table is an open-addressing hash set of nodes by id: local index+1, 0 = empty.
	// At most half full, so probing always terminates.
	table [2 * chunkSize]uint16
}

var seed = maphash.MakeSeed()

func (c *chunk) id(i, w int) []byte { return c.ids[i*w : (i+1)*w] }

// insert adds node i to the table; a later node with the same id replaces the
// earlier one (as a map assignment would).
func (c *chunk) insert(i, w int, h uint64) {
	mask := uint64(len(c.table) - 1)
	for s := h & mask; ; s = (s + 1) & mask {
		v := c.table[s]
		if v == 0 || bytes.Equal(c.id(int(v-1), w), c.id(i, w)) {
			c.table[s] = uint16(i + 1)
			return
		}
	}
}

func (c *chunk) find(id []byte, h uint64) (int, bool) {
	mask := uint64(len(c.table) - 1)
	for s := h & mask; ; s = (s + 1) & mask {
		v := c.table[s]
		if v == 0 {
			return 0, false
		}
		if bytes.Equal(c.id(int(v-1), len(id)), id) {
			return int(v - 1), true
		}
	}
}

// seal drops the append slack of a full chunk.
func (c *chunk) seal() {
	c.parents = append(make([]byte, 0, len(c.parents)), c.parents...)
	c.text = append(make([]byte, 0, len(c.text)), c.text...)
}

type Index struct {
	mu        sync.RWMutex
	w         int // id width in bytes; 0 until the first row
	chunks    []*chunk
	n         int
	people    []string // interned author names and emails
	peopleID  map[string]uint32
	stashRefs map[int32]string // row index -> "stash@{N}"
	cps       []Layout         // cps[k] is the layout state before row k*checkpointEvery
	done      bool
	err       error
}

func NewIndex() *Index {
	return &Index{peopleID: make(map[string]uint32), stashRefs: make(map[int32]string)}
}

func (x *Index) person(s string) uint32 {
	if id, ok := x.peopleID[s]; ok {
		return id
	}
	id := uint32(len(x.people))
	s = strings.Clone(s) // do not keep the caller's record alive
	x.people = append(x.people, s)
	x.peopleID[s] = id
	return id
}

// appendID appends the id s to dst in the index's format.
func (x *Index) appendID(dst []byte, s string) ([]byte, error) {
	n := len(dst)
	dst = append(dst, make([]byte, x.w)...)
	if !encodeID(dst[n:], s) {
		return dst[:n], fmt.Errorf("graph: object id %q does not match the repository's id format", s)
	}
	return dst, nil
}

// append stores rows (their Up/Down are not kept: Rows recomputes them from
// the checkpoints). It stops at the first row whose ids do not have the format
// of the first row's hash.
func (x *Index) append(rows []Row) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	for i := range rows {
		r := &rows[i]
		if x.w == 0 {
			x.w = idWidth(r.Hash)
		}
		if len(x.chunks) == 0 || len(x.chunks[len(x.chunks)-1].nodes) == chunkSize {
			x.chunks = append(x.chunks, &chunk{nodes: make([]node, 0, chunkSize), ids: make([]byte, 0, chunkSize*x.w)})
		}
		c := x.chunks[len(x.chunks)-1]
		ids, err := x.appendID(c.ids, r.Hash)
		if err != nil {
			return err
		}
		npar := len(c.parents)
		for _, p := range r.Parents {
			if c.parents, err = x.appendID(c.parents, p); err != nil {
				c.ids, c.parents = ids[:len(ids)-x.w], c.parents[:npar]
				return err
			}
		}
		c.ids = ids
		c.nodes = append(c.nodes, node{time: r.Time, subject: uint32(len(c.text)), sublen: uint32(len(r.Subject)),
			par: uint32(npar / x.w), npar: uint32(len(r.Parents)),
			author: x.person(r.Author), email: x.person(r.Email), lane: int32(r.Lane), color: uint16(r.Color), kind: r.Kind})
		c.text = append(c.text, r.Subject...)
		k := len(c.nodes) - 1
		c.insert(k, x.w, maphash.Bytes(seed, c.id(k, x.w)))
		if r.StashRef != "" {
			x.stashRefs[int32(x.n)] = r.StashRef
		}
		x.n++
		if len(c.nodes) == chunkSize {
			c.seal()
		}
	}
	return nil
}

func (x *Index) addCheckpoint(l Layout) {
	x.mu.Lock()
	x.cps = append(x.cps, l)
	x.mu.Unlock()
}

func (x *Index) finish(err error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.done, x.err = true, err
}

func (x *Index) Len() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.n
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

func (x *Index) node(i int) (*chunk, int) { return x.chunks[i>>chunkBits], i & (chunkSize - 1) }

// commit returns the hash and parents of row i as strings.
func (x *Index) commit(i int) Commit {
	c, k := x.node(i)
	nd := &c.nodes[k]
	parents := make([]string, nd.npar)
	for j := range parents {
		o := (int(nd.par) + j) * x.w
		parents[j] = decodeID(c.parents[o : o+x.w])
	}
	return Commit{Hash: decodeID(c.id(k, x.w)), Parents: parents}
}

// Rows returns a copy of [start, start+count) clamped to the available rows.
// The segments are recomputed by replaying the layout from the checkpoint at
// or before start; an index filled without checkpoints has no segments.
func (x *Index) Rows(start, count int) []Row {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if start < 0 {
		start = 0
	}
	if start >= x.n || count <= 0 {
		return []Row{}
	}
	end := min(start+count, x.n)
	out := make([]Row, end-start)
	from := start
	var l Layout
	cp := start / checkpointEvery
	replay := cp < len(x.cps)
	if replay {
		l = x.cps[cp].clone()
		from = cp * checkpointEvery
	}
	for i := from; i < end; i++ {
		cm := x.commit(i)
		var p Placement
		if replay {
			p = l.Next(cm)
		}
		if i < start {
			continue
		}
		c, k := x.node(i)
		nd := &c.nodes[k]
		out[i-start] = Row{Hash: cm.Hash, Parents: cm.Parents, Kind: nd.kind, Lane: int(nd.lane), Color: int(nd.color),
			Up: p.Up, Down: p.Down, Author: x.people[nd.author], Email: x.people[nd.email], Time: nd.time,
			Subject: string(c.text[nd.subject : nd.subject+nd.sublen]), StashRef: x.stashRefs[int32(i)]}
	}
	return out
}

func (x *Index) Find(hash string) (int, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if x.w == 0 {
		return 0, false
	}
	var buf [rawIDWidth]byte
	id := buf[:x.w]
	if !encodeID(id, hash) {
		return 0, false
	}
	h := maphash.Bytes(seed, id)
	// Later rows win, as with the former map assignment.
	for k := len(x.chunks) - 1; k >= 0; k-- {
		if i, ok := x.chunks[k].find(id, h); ok {
			return k<<chunkBits + i, true
		}
	}
	return 0, false
}
