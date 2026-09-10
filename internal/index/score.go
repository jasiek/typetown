package index

import (
	"math"

	"typetown/internal/geonames"
)

// featureBonus is GeoNames' own statement of administrative importance,
// expressed as a prominence bonus. It matters because population is zero for
// ~91% of populated places, so it is often the only ranking signal present.
var featureBonus = map[string]float64{
	"PPLC": 5.0, "PPLA": 3.5, "PPLG": 3.0, "PPLCD": 3.0,
	"PPLA2": 2.0, "PPLA3": 1.0, "PPLA4": 0.5, "PPLA5": 0.25,
	"PPL": 0.0, "PPLS": 0.0,
	"PPLL": -0.5, "PPLF": -0.5, "PPLR": -0.5, "STLMT": -0.5, "PPLX": -1.0,
}

// classPenalty demotes a match reached through a lesser name. The penalties are
// bounded on purpose: a hard tier would bury Mumbai when someone types "bombay",
// while no penalty at all lets a prominent city hijack an unrelated prefix
// through an obscure alternate name.
var classPenalty = [4]float64{
	geonames.ClassPrimary:   0.0,
	geonames.ClassOfficial:  1.5,
	geonames.ClassAlternate: 4.0,
	geonames.ClassHistoric:  8.0,
}

// Prominence scores how likely a place is to be the one a user meant, using only
// build-time facts. Three signals, in descending order of availability:
// script diversity (34% of places), feature code (all of them), population (9%).
func Prominence(r *geonames.Record) float64 {
	return math.Log10(float64(r.Population)+1) +
		featureBonus[r.FeatureCode] +
		1.5*math.Log10(float64(r.Scripts)+1)
}

// EdgeScore is the score stored in a key's posting list: prominence, already
// adjusted for how good this particular name is. Both terms are build-time
// constants, which is what lets posting lists be written pre-sorted.
func EdgeScore(prominence float64, class geonames.Class) float64 {
	if int(class) >= len(classPenalty) {
		return prominence
	}
	return prominence - classPenalty[class]
}

// Query-time adjustments. These cannot be baked into the stored score because
// they depend on the caller rather than the data, so they are applied as a
// bounded re-rank over a candidate pool pulled from the merge.
const (
	// ExactBonus rewards a key the query matches in full, rather than merely
	// prefixes: typing "york" should favour York over Yorkville.
	ExactBonus = 1.0
	// HomeBonus favours the caller's own country. Measured against a realistic
	// query set this is the single largest ranking win available: for 3-character
	// prefixes it lifts first-place accuracy from 19% to 56%.
	HomeBonus = 4.0
)
