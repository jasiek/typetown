package geonames

// Class ranks how well a key identifies a place. It is a property of the edge
// from a name to a record, not of the record: Mumbai is reached at ClassPrimary
// through "mumbai" and at ClassHistoric through "bombay".
type Class uint8

const (
	ClassPrimary   Class = iota // the place's own name, or its ascii fold
	ClassOfficial               // preferred name, or one in an official language
	ClassAlternate              // any other alternate name
	ClassHistoric               // a name the place used to have
)

// Record is one populated place, carrying everything a query result needs.
type Record struct {
	ID          int32  // GeoNames geonameid
	Name        string // display name, as GeoNames spells it
	ASCIIName   string // transliteration, may be empty or equal to Name
	CountryCode string // ISO-3166 alpha-2
	Admin1Code  string // raw code; join with an Admin1 table for a display name
	FeatureCode string // PPL, PPLA, PPLC, ...
	Lat, Lon    float64
	Population  int64
	Scripts     uint8 // distinct non-Latin writing systems among alternate names
	AltCount    uint16
}

// Live reports whether a feature code denotes a place that still exists.
// GeoNames keeps historical, abandoned and destroyed settlements in the dump as
// ordinary rows, distinguished only by this code.
func Live(featureCode string) bool {
	switch featureCode {
	case "PPLH", "PPLCH", "PPLQ", "PPLW":
		return false
	}
	return true
}
