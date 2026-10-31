// Package index reads and writes typetown's on-disk autocomplete index.
//
// An index is a directory of four files:
//
//	manifest.json  counts, and the string tables records refer to by number
//	names.fst      finite state transducer: country + folded name -> postings.bin offset
//	postings.bin   per key, the matching records in descending score order
//	records.bin    packed place records, addressed by ordinal
//	records.idx    little-endian uint32 start of each record, plus a sentinel
//	hot.fst        the prefixes with too many keys to scan -> hot.bin offset
//	hot.bin        per hot prefix, a precomputed top-K in descending score order
//
// Keys in names.fst lead with the record's country (see appendCountryKey), so
// every country's names form one contiguous range. A query restricted to some
// countries walks only their ranges; an unrestricted one walks each country's
// range in turn, which costs one seek per country.
//
// The split exists so that a query touches as little as possible: walking a
// prefix reads only the FST, and only the handful of records that survive
// ranking are ever decoded. Every file is mapped read-only rather than read, so
// replicas sharing a volume share one copy of the index in the page cache.
package index

const (
	// FormatVersion is bumped whenever the on-disk encoding changes in a way a
	// previously built index cannot survive. Readers refuse mismatches rather
	// than misinterpreting bytes.
	FormatVersion = 4

	manifestFile = "manifest.json"
	fstFile      = "names.fst"
	postingsFile = "postings.bin"
	recordsFile  = "records.bin"
	recordsIdx   = "records.idx"
	hotFSTFile   = "hot.fst"
	hotFile      = "hot.bin"

	// coordScale converts degrees to the fixed-point integers stored in records.
	// GeoNames publishes five decimal places, i.e. about one metre.
	coordScale = 1e5

	// scoreScale converts scores to the fixed-point integers stored in postings.
	scoreScale = 100
)

// countryKeyLen is how many bytes of a names.fst key name its country.
const countryKeyLen = 2

// maxCountries is how many countries a two-byte key prefix can tell apart.
const maxCountries = 1 << 16

// appendCountryKey appends the names.fst key for a folded name in a country:
// the country's index in the manifest, big-endian so that keys sort country
// first, then the name itself.
func appendCountryKey[T string | []byte](dst []byte, country int, name T) []byte {
	dst = append(dst, byte(country>>8), byte(country))
	return append(dst, name...)
}

// Manifest is the index's self-description. String tables live here rather than
// in records.bin so that "United Kingdom" is stored once instead of 250,000
// times.
type Manifest struct {
	Version      int      `json:"version"`
	Built        string   `json:"built"`
	Source       string   `json:"source"`
	Records      int      `json:"records"`
	Keys         int      `json:"keys"`
	Postings     int      `json:"postings"`
	HotPrefixes  int      `json:"hotPrefixes"`
	HotThreshold int      `json:"hotThreshold"`
	HotK         int      `json:"hotK"`
	Countries    []string `json:"countries"`    // parallel to CountryNames
	CountryNames []string `json:"countryNames"` //
	Regions      []string `json:"regions"`      // display names of admin1 divisions
	FeatureCodes []string `json:"featureCodes"` //
	Filter       string   `json:"filter"`       // human-readable build filter
}

// Result is one ranked answer: a place, and where it is.
type Result struct {
	ID         int32   `json:"id"`
	Name       string  `json:"name"`
	Region     string  `json:"region"`
	Country    string  `json:"country"`
	CC         string  `json:"cc"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Population int64   `json:"population"`
	Kind       string  `json:"kind"`
	Score      float64 `json:"score"`
}
