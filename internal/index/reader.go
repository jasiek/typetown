package index

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/blevesearch/vellum"

	"typetown/internal/geonames"
)

// Index is an open, queryable index.
type Index struct {
	m        *Manifest
	fst      *vellum.FST
	postings []byte
	records  []byte
	offsets  []uint32

	hotFST  *vellum.FST
	hotList []byte
}

// SearchOptions tunes a single query.
type SearchOptions struct {
	Limit int    // results to return; defaults to 10
	Home  string // ISO-3166 alpha-2 of the caller's country; "" disables the bias
	// Pool caps how many candidates a scan may gather. It only applies to
	// prefixes that were not hot enough to earn a precomputed list, which by
	// construction have few keys beneath them.
	Pool int
}

// Open loads an index directory. The FST is memory-mapped by vellum; postings
// and records are read into memory, which for a planet-scale index is a few
// hundred megabytes.
func Open(dir string) (*Index, error) {
	mf, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(mf, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Version != FormatVersion {
		return nil, fmt.Errorf("index format v%d, this build reads v%d; rebuild the index", m.Version, FormatVersion)
	}
	fst, err := vellum.Open(filepath.Join(dir, fstFile))
	if err != nil {
		return nil, fmt.Errorf("open fst: %w", err)
	}
	postings, err := os.ReadFile(filepath.Join(dir, postingsFile))
	if err != nil {
		return nil, fmt.Errorf("open postings: %w", err)
	}
	records, err := os.ReadFile(filepath.Join(dir, recordsFile))
	if err != nil {
		return nil, fmt.Errorf("open records: %w", err)
	}
	idx := &Index{m: &m, fst: fst, postings: postings, records: records}
	if err := idx.buildOffsets(); err != nil {
		return nil, err
	}
	idx.hotFST, err = vellum.Open(filepath.Join(dir, hotFSTFile))
	if err != nil {
		return nil, fmt.Errorf("open hot fst: %w", err)
	}
	idx.hotList, err = os.ReadFile(filepath.Join(dir, hotFile))
	if err != nil {
		return nil, fmt.Errorf("open hot lists: %w", err)
	}
	return idx, nil
}

// buildOffsets walks records.bin once at open time to recover each record's
// start. Storing an offset table on disk would trade 20 MB of file for this
// ~100 ms scan; the scan wins because the file is read sequentially anyway.
func (ix *Index) buildOffsets() error {
	ix.offsets = make([]uint32, 0, ix.m.Records+1)
	var p uint32
	for i := 0; i < ix.m.Records; i++ {
		ix.offsets = append(ix.offsets, p)
		n, err := recordLen(ix.records[p:])
		if err != nil {
			return fmt.Errorf("record %d: %w", i, err)
		}
		p += uint32(n)
	}
	ix.offsets = append(ix.offsets, p)
	if int(p) != len(ix.records) {
		return fmt.Errorf("records.bin: consumed %d of %d bytes", p, len(ix.records))
	}
	return nil
}

func (ix *Index) Manifest() *Manifest { return ix.m }

func (ix *Index) Close() error {
	err := ix.fst.Close()
	if herr := ix.hotFST.Close(); err == nil {
		err = herr
	}
	return err
}

type candidate struct {
	ord   uint32
	score float64
}

// Search returns the best matches for a query, ranked.
//
// The query is folded the same way keys were at build time, then used as a
// prefix over the FST. Each matching key yields a posting list already in
// descending score order, so ranking is a merge rather than a scan.
func (ix *Index) Search(query string, opts SearchOptions) ([]Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.Pool <= 0 {
		opts.Pool = 20000
	}
	q := geonames.Fold(query)
	if q == "" {
		return nil, nil
	}

	best := make(map[uint32]float64, 1024)
	keep := func(ord uint32, score float64) {
		if cur, ok := best[ord]; !ok || score > cur {
			best[ord] = score
		}
	}

	hot, err := ix.gatherHot(q, keep)
	if err != nil {
		return nil, err
	}
	if !hot {
		if err := ix.gatherScan(q, opts.Pool, keep); err != nil {
			return nil, err
		}
	}

	cands := make([]candidate, 0, len(best))
	for ord, s := range best {
		cands = append(cands, candidate{ord, s})
	}
	// The home-country bonus depends on the caller, so it cannot be baked into
	// the stored scores; it is applied here, over the pool, before trimming.
	if opts.Home != "" {
		if home := ix.countryIndex(opts.Home); home >= 0 {
			for i := range cands {
				if ix.countryOf(cands[i].ord) == home {
					cands[i].score += HomeBonus
				}
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].ord < cands[j].ord
	})
	if len(cands) > opts.Limit {
		cands = cands[:opts.Limit]
	}
	out := make([]Result, 0, len(cands))
	for _, c := range cands {
		r, err := ix.decode(c.ord)
		if err != nil {
			return nil, err
		}
		r.Score = c.score
		out = append(out, r)
	}
	return out, nil
}

// gatherHot serves a prefix that has a precomputed shortlist, and reports
// whether it did. The shortlist is a ranked sample rather than the whole range,
// so an exact hit on the query itself is added separately: it carries a bonus
// the build-time score could not know about.
func (ix *Index) gatherHot(q string, keep func(uint32, float64)) (bool, error) {
	off, ok, err := ix.hotFST.Get([]byte(q))
	if err != nil {
		return false, fmt.Errorf("hot lookup %q: %w", q, err)
	}
	if !ok {
		return false, nil
	}
	if err := ix.eachEntry(ix.hotList, off, func(ord uint32, score float64) bool {
		keep(ord, score)
		return true
	}); err != nil {
		return false, err
	}
	if poff, exact, err := ix.fst.Get([]byte(q)); err != nil {
		return false, fmt.Errorf("exact lookup %q: %w", q, err)
	} else if exact {
		if err := ix.eachEntry(ix.postings, poff, func(ord uint32, score float64) bool {
			keep(ord, score+ExactBonus)
			return true
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

// gatherScan walks every key under a prefix. It is only reached for prefixes
// that were not hot, which by construction have few keys beneath them, so the
// pool ceiling is a safety net rather than a sampling policy.
func (ix *Index) gatherScan(q string, pool int, keep func(uint32, float64)) error {
	n := 0
	it, err := ix.fst.Iterator([]byte(q), prefixSuccessor(q))
	for err == nil {
		key, off := it.Current()
		bonus := 0.0
		if len(key) == len(q) {
			bonus = ExactBonus
		}
		if perr := ix.eachEntry(ix.postings, off, func(ord uint32, score float64) bool {
			keep(ord, score+bonus)
			n++
			return n < pool
		}); perr != nil {
			return perr
		}
		if n >= pool {
			break
		}
		err = it.Next()
	}
	if err != nil && err != vellum.ErrIteratorDone {
		return fmt.Errorf("search %q: %w", q, err)
	}
	return nil
}

func (ix *Index) countryIndex(cc string) int {
	for i, c := range ix.m.Countries {
		if c == cc {
			return i
		}
	}
	return -1
}

// eachEntry decodes a (count, then count x (ordinal, score)) list. Both
// postings.bin and hot.bin use this encoding.
func (ix *Index) eachEntry(src []byte, off uint64, fn func(ord uint32, score float64) bool) error {
	if off >= uint64(len(src)) {
		return fmt.Errorf("list offset %d out of range", off)
	}
	buf := src[off:]
	n, w := binary.Uvarint(buf)
	if w <= 0 {
		return fmt.Errorf("list offset %d: bad count", off)
	}
	buf = buf[w:]
	for i := uint64(0); i < n; i++ {
		ord, w1 := binary.Uvarint(buf)
		if w1 <= 0 {
			return fmt.Errorf("list offset %d: bad ordinal", off)
		}
		buf = buf[w1:]
		sc, w2 := binary.Varint(buf)
		if w2 <= 0 {
			return fmt.Errorf("list offset %d: bad score", off)
		}
		buf = buf[w2:]
		if !fn(uint32(ord), float64(sc)/scoreScale) {
			return nil
		}
	}
	return nil
}

// countryOf decodes just far enough into a record to read its country index.
func (ix *Index) countryOf(ord uint32) int {
	b := ix.records[ix.offsets[ord]:ix.offsets[ord+1]]
	for i := 0; i < 5; i++ { // id, lat, lon, population, feature
		_, w := binary.Uvarint(b)
		if w <= 0 {
			return -1
		}
		b = b[w:]
	}
	cc, w := binary.Uvarint(b)
	if w <= 0 {
		return -1
	}
	return int(cc)
}

func (ix *Index) decode(ord uint32) (Result, error) {
	if int(ord)+1 >= len(ix.offsets) {
		return Result{}, fmt.Errorf("record ordinal %d out of range", ord)
	}
	b := ix.records[ix.offsets[ord]:ix.offsets[ord+1]]
	read := func() (uint64, error) {
		v, w := binary.Uvarint(b)
		if w <= 0 {
			return 0, fmt.Errorf("record %d: truncated", ord)
		}
		b = b[w:]
		return v, nil
	}
	readSigned := func() (int64, error) {
		v, w := binary.Varint(b)
		if w <= 0 {
			return 0, fmt.Errorf("record %d: truncated", ord)
		}
		b = b[w:]
		return v, nil
	}
	id, err := read()
	if err != nil {
		return Result{}, err
	}
	lat, err := readSigned()
	if err != nil {
		return Result{}, err
	}
	lon, err := readSigned()
	if err != nil {
		return Result{}, err
	}
	pop, err := read()
	if err != nil {
		return Result{}, err
	}
	fc, err := read()
	if err != nil {
		return Result{}, err
	}
	cc, err := read()
	if err != nil {
		return Result{}, err
	}
	reg, err := read()
	if err != nil {
		return Result{}, err
	}
	nlen, err := read()
	if err != nil {
		return Result{}, err
	}
	if uint64(len(b)) < nlen {
		return Result{}, fmt.Errorf("record %d: name truncated", ord)
	}
	res := Result{
		ID: int32(id), Name: string(b[:nlen]),
		Lat: float64(lat) / coordScale, Lon: float64(lon) / coordScale,
		Population: int64(pop),
	}
	if int(fc) < len(ix.m.FeatureCodes) {
		res.Kind = ix.m.FeatureCodes[fc]
	}
	if int(cc) < len(ix.m.Countries) {
		res.CC = ix.m.Countries[cc]
		res.Country = ix.m.CountryNames[cc]
	}
	if reg > 0 && int(reg) < len(ix.m.Regions) {
		res.Region = ix.m.Regions[reg]
	}
	return res, nil
}

// recordLen reports the encoded size of the record starting at b, by walking
// its eight varint fields and adding the trailing name.
func recordLen(b []byte) (int, error) {
	p := 0
	var namelen uint64
	for i := 0; i < 8; i++ {
		v, w := binary.Uvarint(b[p:])
		if w <= 0 {
			return 0, fmt.Errorf("truncated field %d", i)
		}
		p += w
		namelen = v // the eighth and last field is the name length
	}
	return p + int(namelen), nil
}

// prefixSuccessor returns the exclusive upper bound of a prefix range: the
// smallest byte string greater than every string starting with p.
func prefixSuccessor(p string) []byte {
	b := []byte(p)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			out := make([]byte, i+1)
			copy(out, b[:i+1])
			out[i]++
			return out
		}
	}
	return nil // p is all 0xff: no upper bound
}
