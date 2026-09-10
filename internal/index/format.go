// Package index reads and writes typetown's on-disk autocomplete index.
//
// An index is a directory of four files:
//
//	manifest.json  counts, and the string tables records refer to by number
//	names.fst      finite state transducer: folded name -> postings.bin offset
//	postings.bin   per key, the matching records in descending score order
//	records.bin    packed place records, addressed by ordinal
//
// The split exists so that a query touches as little as possible: walking a
// prefix reads only the FST, and only the handful of records that survive
// ranking are ever decoded.
package index

const (
	// FormatVersion is bumped whenever the on-disk encoding changes in a way a
	// previously built index cannot survive. Readers refuse mismatches rather
	// than misinterpreting bytes.
	FormatVersion = 1

	manifestFile = "manifest.json"
	fstFile      = "names.fst"
	postingsFile = "postings.bin"
	recordsFile  = "records.bin"

	// coordScale converts degrees to the fixed-point integers stored in records.
	// GeoNames publishes five decimal places, i.e. about one metre.
	coordScale = 1e5

	// scoreScale converts scores to the fixed-point integers stored in postings.
	scoreScale = 100
)

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
