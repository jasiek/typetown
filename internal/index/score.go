package index

import (
	"math"

	"github.com/jasiek/typetown/internal/geonames"
)

// featureWeight scales the feature-code bonus below. It is small on purpose:
// at full strength a national capital outranked a place with 100,000 more
// residents, which measured worse in every country tested.
const featureWeight = 0.25

// featureBonus is GeoNames' own statement of administrative importance. It
// breaks ties among places whose population is unrecorded, which is most of
// them, but it is a tiebreak rather than a rival to population.
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
//
// ClassHistoric is capped just above HomeBonus rather than at twice it. At 8.0,
// a home-country bias was enough to put Bombay Beach, California (pop 295) above
// Mumbai for the query "bombay" -- the two query-time bonuses have to stay small
// relative to the penalties they interact with.
//
// These are calibrated against the range Prominence actually spans, so they were
// halved when that range was: penalties and bonuses sized for a 0-17 scale are
// twice as forceful on a 0-9 one.
var classPenalty = [4]float64{
	geonames.ClassPrimary:   0.0,
	geonames.ClassOfficial:  0.75,
	geonames.ClassAlternate: 2.0,
	geonames.ClassHistoric:  2.5,
}

// Prominence scores how likely a place is to be the one a user meant, using only
// build-time facts.
//
// Population leads, despite being recorded for just 9% of populated places. The
// availability statistic is a fact about the dataset, not about the queries: the
// places people actually look up are the ones that have a population figure, and
// the ones that do not are the tail nobody searches for.
//
// Measured against a query set weighted to real traffic across 24 countries,
// population alone beat every combination tried, and adding the feature code at
// a quarter weight beat population alone. A third signal -- how many writing
// systems name the place -- was tried and removed: it lowered accuracy in all 24
// countries, including the ones whose own script is not Latin.
func Prominence(r *geonames.Record) float64 {
	return math.Log10(float64(r.Population)+1) + featureWeight*featureBonus[r.FeatureCode]
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
	ExactBonus = 0.5
	// HomeBonus favours the caller's own country. Measured against a realistic
	// query set this is the single largest ranking win available: for 3-character
	// prefixes it lifts first-place accuracy from 19% to 56%. It must stay well
	// under the span of Prominence, or a nearby village outranks a world city.
	HomeBonus = 2.0
)

// Proximity.
//
// A caller's coordinates say more than their country: they distinguish two
// places within one country, which a country code cannot, and they get the
// border case right, where the nearest London to somebody in Detroit really is
// the one in Ontario.
//
// The bonus adds to the country bonus rather than replacing it. Replacement was
// tried first and measured worse than no coordinates at all — at a typical
// distance proximity is worth less than the flat country bonus, so supplying a
// better signal lowered the score. Taking the larger of the two was no better,
// because the flat bonus then swamps proximity and two towns in the caller's own
// country stop being distinguishable.
const (
	// NearBonus is the most that being close can be worth. It adds to HomeBonus
	// rather than replacing it, because knowing the country and knowing the
	// distance are different facts.
	//
	// Measured against callers placed near the town they were looking for, higher
	// values keep scoring better without limit — but that is an artefact of the
	// query set, which assumes most callers are near their answer. What bounds it
	// is the judgement the benchmark cannot make: at 4.0, standing in London,
	// Kentucky puts it above London. 3.0 leaves a point of headroom below that.
	NearBonus = 3.0
	// NearRange is the distance in kilometres past which proximity stops
	// counting for anything. Measurement barely distinguished 500 from 3000, so
	// this is the middle of a flat optimum rather than a tuned value.
	NearRange = 1000.0

	earthRadiusKm = 6371.0088
)

// nearness converts a distance into a bonus, on a log scale because that is how
// distance is perceived: the gap between 5 km and 50 km matters far more than
// the gap between 500 km and 550 km.
func nearness(km float64) float64 {
	if km <= 0 {
		return NearBonus
	}
	if km >= NearRange {
		return 0
	}
	f := 1 - math.Log10(1+km)/math.Log10(1+NearRange)
	if f <= 0 {
		return 0
	}
	return NearBonus * f
}

// distanceKm is the great-circle distance between two points.
func distanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp := p2 - p1
	dl := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(a))
}
