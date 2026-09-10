package index

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/blevesearch/vellum"

	"typetown/internal/geonames"
)

// BuildOptions configures a build.
type BuildOptions struct {
	DumpPath     string // allCountries.zip
	AltNamesPath string // alternateNamesV2.zip; optional but strongly recommended
	Admin1Path   string
	CountryPath  string
	OutDir       string
	Filter       geonames.Filter
	Progress     func(stage string, n int)
}

// keyRef is one edge from a name to a record, held in a flat slice with the key
// bytes in a shared arena. 16.6M of these are live at once during a full build,
// so the struct is kept to 12 bytes and the strings are not.
type keyRef struct {
	off   uint32 // into arena
	ln    uint16
	class uint8
	ord   uint32 // record ordinal
}

type builder struct {
	opts   BuildOptions
	tables geonames.Tables

	arena []byte
	keys  []keyRef

	recOffsets []uint32  // ordinal -> offset into records.bin
	prominence []float32 // ordinal -> prominence
	idToOrd    map[int32]uint32

	countries    []string
	countryNames []string
	countryIdx   map[string]int
	regions      []string
	regionIdx    map[string]int
	features     []string
	featureIdx   map[string]int
}

// Build reads the GeoNames inputs and writes a queryable index into OutDir.
func Build(opts BuildOptions) (*Manifest, error) {
	if opts.Progress == nil {
		opts.Progress = func(string, int) {}
	}
	tables, err := geonames.LoadTables(opts.Admin1Path, opts.CountryPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("create index dir: %w", err)
	}

	b := &builder{
		opts: opts, tables: tables,
		idToOrd:    make(map[int32]uint32, 1<<20),
		countryIdx: map[string]int{}, regionIdx: map[string]int{}, featureIdx: map[string]int{},
	}
	if err := b.writeRecords(); err != nil {
		return nil, err
	}
	if err := b.addAltNames(); err != nil {
		return nil, err
	}
	return b.writeIndex()
}

// writeRecords streams the dump straight to records.bin, emitting the primary
// and ascii keys as it goes. Nothing is held in memory except the offsets, the
// prominence scores and the key arena.
func (b *builder) writeRecords() error {
	f, err := os.Create(filepath.Join(b.opts.OutDir, recordsFile))
	if err != nil {
		return fmt.Errorf("create records: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)

	var off uint32
	var buf []byte
	err = geonames.ReadDump(b.opts.DumpPath, b.opts.Filter, func(r *geonames.Record) error {
		ord := uint32(len(b.recOffsets))
		b.recOffsets = append(b.recOffsets, off)
		b.idToOrd[r.ID] = ord

		p := Prominence(r)
		b.prominence = append(b.prominence, float32(p))

		buf = b.encodeRecord(buf[:0], r)
		n, err := w.Write(buf)
		if err != nil {
			return fmt.Errorf("write record: %w", err)
		}
		off += uint32(n)

		b.addKey(r.Name, ord, geonames.ClassPrimary)
		if r.ASCIIName != "" && r.ASCIIName != r.Name {
			b.addKey(r.ASCIIName, ord, geonames.ClassPrimary)
		}
		if len(b.recOffsets)%500_000 == 0 {
			b.opts.Progress("records", len(b.recOffsets))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush records: %w", err)
	}
	b.recOffsets = append(b.recOffsets, off) // sentinel: end of the last record
	b.opts.Progress("records", len(b.recOffsets)-1)
	return nil
}

func (b *builder) addAltNames() error {
	if b.opts.AltNamesPath == "" {
		return nil
	}
	// A name in one of the country's own languages counts as official.
	official := func(gid int32, lang string) bool {
		return lang == "en"
	}
	n := 0
	err := geonames.ReadAltNames(b.opts.AltNamesPath,
		func(gid int32) bool { _, ok := b.idToOrd[gid]; return ok },
		official,
		func(a geonames.AltName) error {
			ord, ok := b.idToOrd[a.GeonameID]
			if !ok {
				return nil
			}
			b.addKey(a.Name, ord, a.Class)
			n++
			if n%2_000_000 == 0 {
				b.opts.Progress("alt names", n)
			}
			return nil
		})
	if err != nil {
		return err
	}
	b.opts.Progress("alt names", n)
	return nil
}

func (b *builder) addKey(name string, ord uint32, class geonames.Class) {
	k := geonames.Fold(name)
	if k == "" || len(k) > 255 {
		return // empty, or long enough to be data error rather than a place name
	}
	b.keys = append(b.keys, keyRef{
		off: uint32(len(b.arena)), ln: uint16(len(k)), class: uint8(class), ord: ord,
	})
	b.arena = append(b.arena, k...)
}

func (b *builder) key(r keyRef) []byte { return b.arena[r.off : r.off+uint32(r.ln)] }

// writeIndex sorts the keys, writes each key's posting list in descending score
// order, and builds the FST over the keys pointing at those lists.
func (b *builder) writeIndex() (*Manifest, error) {
	b.opts.Progress("sorting keys", len(b.keys))
	slices.SortFunc(b.keys, func(x, y keyRef) int {
		if c := strings.Compare(string(b.key(x)), string(b.key(y))); c != 0 {
			return c
		}
		// Highest score first, so a posting list is written already sorted.
		sx := EdgeScore(float64(b.prominence[x.ord]), geonames.Class(x.class))
		sy := EdgeScore(float64(b.prominence[y.ord]), geonames.Class(y.class))
		switch {
		case sx > sy:
			return -1
		case sx < sy:
			return 1
		}
		return int(x.ord) - int(y.ord)
	})

	pf, err := os.Create(filepath.Join(b.opts.OutDir, postingsFile))
	if err != nil {
		return nil, fmt.Errorf("create postings: %w", err)
	}
	defer pf.Close()
	pw := bufio.NewWriterSize(pf, 1<<20)

	ff, err := os.Create(filepath.Join(b.opts.OutDir, fstFile))
	if err != nil {
		return nil, fmt.Errorf("create fst: %w", err)
	}
	defer ff.Close()
	fw := bufio.NewWriterSize(ff, 1<<20)
	fst, err := vellum.New(fw, nil)
	if err != nil {
		return nil, fmt.Errorf("new fst: %w", err)
	}

	var (
		poff     uint64
		nkeys    int
		npost    int
		scratch  []byte
		postings []keyRef
	)
	flush := func() error {
		if len(postings) == 0 {
			return nil
		}
		scratch = scratch[:0]
		scratch = binary.AppendUvarint(scratch, uint64(len(postings)))
		// Ordinals are stored plainly rather than delta-coded: a list is sorted
		// by score, not by ordinal, so the deltas would not be monotonic.
		for _, p := range postings {
			scratch = binary.AppendUvarint(scratch, uint64(p.ord))
			s := EdgeScore(float64(b.prominence[p.ord]), geonames.Class(p.class))
			scratch = binary.AppendVarint(scratch, int64(s*scoreScale))
		}
		if err := fst.Insert(b.key(postings[0]), poff); err != nil {
			return fmt.Errorf("fst insert: %w", err)
		}
		n, err := pw.Write(scratch)
		if err != nil {
			return fmt.Errorf("write postings: %w", err)
		}
		poff += uint64(n)
		nkeys++
		npost += len(postings)
		postings = postings[:0]
		return nil
	}

	for i, k := range b.keys {
		if i > 0 && !slices.Equal(b.key(k), b.key(b.keys[i-1])) {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		// One record can reach the same key by several names; keep its best.
		dup := false
		for _, p := range postings {
			if p.ord == k.ord {
				dup = true
				break
			}
		}
		if !dup {
			postings = append(postings, k)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if err := fst.Close(); err != nil {
		return nil, fmt.Errorf("close fst: %w", err)
	}
	if err := fw.Flush(); err != nil {
		return nil, fmt.Errorf("flush fst: %w", err)
	}
	if err := pw.Flush(); err != nil {
		return nil, fmt.Errorf("flush postings: %w", err)
	}
	b.opts.Progress("keys", nkeys)

	m := &Manifest{
		Version: FormatVersion, Built: time.Now().UTC().Format(time.RFC3339),
		Source: "geonames", Records: len(b.recOffsets) - 1, Keys: nkeys, Postings: npost,
		Countries: b.countries, CountryNames: b.countryNames,
		Regions: b.regions, FeatureCodes: b.features,
		Filter: describeFilter(b.opts.Filter),
	}
	return m, b.writeManifest(m)
}

func (b *builder) writeManifest(m *Manifest) error {
	f, err := os.Create(filepath.Join(b.opts.OutDir, manifestFile))
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// encodeRecord packs one place. Strings that repeat across millions of records
// (country, region, feature code) are stored as indexes into manifest tables.
func (b *builder) encodeRecord(dst []byte, r *geonames.Record) []byte {
	dst = binary.AppendUvarint(dst, uint64(r.ID))
	dst = binary.AppendVarint(dst, int64(r.Lat*coordScale))
	dst = binary.AppendVarint(dst, int64(r.Lon*coordScale))
	dst = binary.AppendUvarint(dst, uint64(r.Population))
	dst = binary.AppendUvarint(dst, uint64(b.internFeature(r.FeatureCode)))
	dst = binary.AppendUvarint(dst, uint64(b.internCountry(r.CountryCode)))
	dst = binary.AppendUvarint(dst, uint64(b.internRegion(b.tables.Region(r.CountryCode, r.Admin1Code))))
	dst = binary.AppendUvarint(dst, uint64(len(r.Name)))
	return append(dst, r.Name...)
}

func (b *builder) internFeature(code string) int {
	if i, ok := b.featureIdx[code]; ok {
		return i
	}
	b.featureIdx[code] = len(b.features)
	b.features = append(b.features, code)
	return len(b.features) - 1
}

func (b *builder) internCountry(cc string) int {
	if i, ok := b.countryIdx[cc]; ok {
		return i
	}
	b.countryIdx[cc] = len(b.countries)
	b.countries = append(b.countries, cc)
	b.countryNames = append(b.countryNames, b.tables.Country(cc))
	return len(b.countries) - 1
}

// internRegion reserves index 0 for "no region", which is a real answer for
// city-states rather than a missing value.
func (b *builder) internRegion(name string) int {
	if name == "" {
		return 0
	}
	if i, ok := b.regionIdx[name]; ok {
		return i
	}
	if len(b.regions) == 0 {
		b.regions = append(b.regions, "")
	}
	b.regionIdx[name] = len(b.regions)
	b.regions = append(b.regions, name)
	return len(b.regions) - 1
}

func describeFilter(f geonames.Filter) string {
	var parts []string
	if f.Countries != nil {
		cc := make([]string, 0, len(f.Countries))
		for c := range f.Countries {
			cc = append(cc, c)
		}
		sort.Strings(cc)
		parts = append(parts, "countries="+strings.Join(cc, ","))
	}
	if f.MinPopulation > 0 {
		parts = append(parts, fmt.Sprintf("min-population=%d", f.MinPopulation))
	}
	if f.IncludeParts {
		parts = append(parts, "include-parts")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}
