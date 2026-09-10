// Package typetown turns a typed prefix into a place: a town, the division and
// country it sits in, and its coordinates.
//
// It is meant to be embedded. An index is a directory of files built ahead of
// time by the typetown command; opening one memory-maps it, so several processes
// sharing a volume share a single copy in the host page cache and a query costs
// tens of microseconds with no network hop.
//
//	ix, err := typetown.Open("/srv/typetown/index")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer ix.Close()
//
//	http.Handle("/places", typetown.Handler(ix, typetown.WithHome("US")))
//
// Search is safe for concurrent use; Close is not, and must not race with it.
package typetown

import (
	"fmt"

	"github.com/jasiek/typetown/internal/index"
)

// Index is an open, queryable place index.
type Index struct {
	ix *index.Index
}

// Options configure how an index is opened.
type Options struct {
	// NoMmap reads the index onto the heap instead of memory-mapping it.
	//
	// Mapping is the default because it is what lets replicas share one copy of
	// the index, and because file-backed pages are reclaimable under memory
	// pressure rather than a reason to be OOM-killed. Read instead when you need
	// a snapshot that cannot change underneath you — a mapping is a live view, so
	// rewriting an index in place produces torn reads.
	NoMmap bool
}

// Open opens an index directory, memory-mapping its files.
func Open(dir string) (*Index, error) { return OpenWith(dir, Options{}) }

// OpenWith opens an index directory with explicit options.
func OpenWith(dir string, opts Options) (*Index, error) {
	ix, err := index.OpenWith(dir, index.OpenOptions{NoMmap: opts.NoMmap})
	if err != nil {
		return nil, err
	}
	return &Index{ix: ix}, nil
}

// Close releases the index. It must not be called while a Search is in flight.
func (x *Index) Close() error {
	if x == nil || x.ix == nil {
		return nil
	}
	err := x.ix.Close()
	x.ix = nil
	return err
}

// LatLon is a point on the earth, in decimal degrees.
type LatLon struct {
	Lat float64
	Lon float64
}

// SearchOptions tune a single query.
type SearchOptions struct {
	// Limit is how many results to return. Zero means ten.
	Limit int
	// Home is an ISO-3166 alpha-2 country code to bias results toward, or "" for
	// no bias. It is a nudge, not an override: a nearby village will not displace
	// a world city, but among comparable places the local one wins.
	Home string
	// Near is where the caller is. It separates two places within one country,
	// which a country code cannot, and it gets the border case right: the nearest
	// London to somebody in Detroit is the one in Ontario.
	//
	// It combines with Home rather than replacing it — whichever of the two helps
	// a given place more is the one that counts — so setting both is safe and
	// setting either can only improve the ranking.
	//
	// Like Home it is bounded, so a village down the road will not displace a
	// capital. Its effect falls away logarithmically and reaches nothing at about
	// a thousand kilometres.
	Near *LatLon
	// Pool caps how many candidates a scan gathers before ranking. Zero picks a
	// sensible default. Raising it costs latency on short prefixes.
	Pool int
}

// Result is one ranked place.
type Result struct {
	// ID is the GeoNames geonameid — stable across index rebuilds, and the value
	// to carry into whatever the coordinates are handed to.
	ID int32 `json:"id"`
	// Name is the place as GeoNames spells it, which may not be ASCII.
	Name string `json:"name"`
	// Region is the first-order division: a state, province or country. Empty
	// when the place genuinely has none, as for a city-state.
	Region string `json:"region,omitempty"`
	// Country is the display name; CC is its ISO-3166 alpha-2 code.
	Country string `json:"country"`
	CC      string `json:"cc"`
	// Lat and Lon are WGS84 decimal degrees, to five places.
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	// Population is zero when GeoNames does not record one, which is common
	// outside towns of any size.
	Population int64 `json:"population,omitempty"`
	// Kind is the GeoNames feature code: PPL, PPLA, PPLC and so on.
	Kind string `json:"kind"`
	// Score is the rank score. Comparable within one result set, and not
	// meaningful between them.
	Score float64 `json:"score"`
}

// Search returns the best places whose name starts with query, best first.
//
// The query is folded the same way names were when the index was built: case
// and diacritics are ignored, so "zurich" reaches "Zürich". Matching is on a
// whole name, not on a word within one, so "york" does not reach "New York".
func (x *Index) Search(query string, opts SearchOptions) ([]Result, error) {
	if x == nil || x.ix == nil {
		return nil, fmt.Errorf("typetown: index is closed")
	}
	var near *index.LatLon
	if opts.Near != nil {
		near = &index.LatLon{Lat: opts.Near.Lat, Lon: opts.Near.Lon}
	}
	hits, err := x.ix.Search(query, index.SearchOptions{
		Limit: opts.Limit, Home: opts.Home, Near: near, Pool: opts.Pool,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Result, len(hits))
	for i, h := range hits {
		out[i] = Result{
			ID: h.ID, Name: h.Name, Region: h.Region, Country: h.Country, CC: h.CC,
			Lat: h.Lat, Lon: h.Lon, Population: h.Population, Kind: h.Kind, Score: h.Score,
		}
	}
	return out, nil
}

// Stats describes what an index holds.
type Stats struct {
	Version     int    `json:"version"`
	Built       string `json:"built"`
	Records     int    `json:"records"`
	Keys        int    `json:"keys"`
	Postings    int    `json:"postings"`
	HotPrefixes int    `json:"hotPrefixes"`
	Filter      string `json:"filter"`
}

// Stats reports what the open index holds.
func (x *Index) Stats() Stats {
	if x == nil || x.ix == nil {
		return Stats{}
	}
	m := x.ix.Manifest()
	return Stats{
		Version: m.Version, Built: m.Built, Records: m.Records, Keys: m.Keys,
		Postings: m.Postings, HotPrefixes: m.HotPrefixes, Filter: m.Filter,
	}
}
