package geonames

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// folder decomposes runes, drops combining marks, then recomposes: "Rāmpur"
// becomes "rampur" so a user typing plain ASCII reaches diacritic-bearing names.
var folder = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// Fold normalises a name into an index key: case-folded, diacritic-stripped,
// with runs of whitespace collapsed. Keys are compared as raw bytes by the FST,
// so this is the only place normalisation happens — a query and the key it must
// match both pass through here.
func Fold(s string) string {
	out, _, err := transform.String(folder, s)
	if err != nil {
		out = s // transform only fails on malformed input; fall back to raw
	}
	return strings.Join(strings.Fields(strings.ToLower(out)), " ")
}
