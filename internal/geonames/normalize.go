package geonames

import (
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// folders hands out transformers that decompose runes, drop combining marks and
// recompose, so "Rāmpur" becomes "rampur" and a user typing plain ASCII reaches
// a name that is not.
//
// They are pooled rather than shared: a transform.Chain carries state across the
// call and resets it on entry, so one shared instance is a data race between
// concurrent callers — which Fold has, since Search is called from every request
// an HTTP handler serves.
var folders = sync.Pool{
	New: func() any {
		return transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	},
}

// Fold normalises a name into an index key: case-folded, diacritic-stripped,
// with runs of whitespace collapsed. Keys are compared as raw bytes by the FST,
// so this is the only place normalisation happens — a query and the key it must
// match both pass through here.
//
// Fold is safe for concurrent use.
func Fold(s string) string {
	t := folders.Get().(transform.Transformer)
	out, _, err := transform.String(t, s)
	folders.Put(t)
	if err != nil {
		out = s // transform only fails on malformed input; fall back to raw
	}
	return strings.Join(strings.Fields(strings.ToLower(out)), " ")
}
