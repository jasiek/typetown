package index

import "container/heap"

// Hot prefixes.
//
// A prefix query has to consider every key beneath it, and for a short prefix
// that is hundreds of thousands of keys — far too slow to run on each keystroke.
// Capping the scan is not an answer: the FST yields keys alphabetically, so a
// cap silently samples by spelling rather than by score, and typing "p" returns
// Pa Sang, Thailand instead of Paris.
//
// Almost all of that cost sits in a handful of prefixes. Only ~7,000 prefixes
// have as many as hotThreshold keys beneath them, so those get a precomputed
// top-hotK list at build time and everything else is scanned exhaustively —
// which is cheap precisely because it is not hot.
const (
	defaultHotThreshold = 500 // keys beneath a prefix before it earns a cached list
	hotK                = 256 // entries kept per hot prefix
	maxPrefixLen        = 16  // longest prefix considered; the longest hot one is ~12
)

// topK keeps the best hotK (ordinal, score) pairs seen for one prefix, keeping a
// record's highest score when it is reachable under that prefix by several
// names. It is a min-heap on score — the cheapest entry to evict sits at index
// 0 — with an index alongside it so a repeated ordinal can be updated in place.
type topK struct {
	e   []candidate
	pos map[uint32]int
}

func newTopK() *topK { return &topK{pos: make(map[uint32]int, hotK)} }

func (t *topK) Len() int           { return len(t.e) }
func (t *topK) Less(i, j int) bool { return t.e[i].score < t.e[j].score }
func (t *topK) Swap(i, j int) {
	t.e[i], t.e[j] = t.e[j], t.e[i]
	t.pos[t.e[i].ord] = i
	t.pos[t.e[j].ord] = j
}
func (t *topK) Push(x any) {
	c := x.(candidate)
	t.pos[c.ord] = len(t.e)
	t.e = append(t.e, c)
}
func (t *topK) Pop() any {
	c := t.e[len(t.e)-1]
	t.e = t.e[:len(t.e)-1]
	delete(t.pos, c.ord)
	return c
}

func (t *topK) add(ord uint32, score float64) {
	if i, ok := t.pos[ord]; ok {
		if t.e[i].score < score {
			t.e[i].score = score
			heap.Fix(t, i)
		}
		return
	}
	if len(t.e) < hotK {
		heap.Push(t, candidate{ord, score})
		return
	}
	if score <= t.e[0].score {
		return
	}
	delete(t.pos, t.e[0].ord)
	t.e[0] = candidate{ord, score}
	t.pos[ord] = 0
	heap.Fix(t, 0)
}

// min reports the lowest score currently retained, or -inf while there is room.
// Callers use it to stop reading a key's postings once they can no longer help.
func (t *topK) min() float64 {
	if len(t.e) < hotK {
		return -1e18
	}
	return t.e[0].score
}

func (t *topK) reset() {
	t.e = t.e[:0]
	clear(t.pos)
}

// drain returns the retained candidates in descending score order.
func (t *topK) drain() []candidate {
	out := make([]candidate, len(t.e))
	copy(out, t.e)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].score > out[j-1].score; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// commonPrefixLen reports how many leading bytes two keys share.
func commonPrefixLen(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
