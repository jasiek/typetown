package geonames

import "unicode"

// scriptRanges maps codepoint ranges to a small script id. Latin (including
// ASCII) is deliberately absent: we are counting how many *other* writing
// systems name a place.
var scriptRanges = []struct {
	lo, hi rune
	id     uint8
}{
	{0x0370, 0x03FF, 1}, {0x0400, 0x052F, 2}, {0x0530, 0x058F, 3},
	{0x0590, 0x05FF, 4}, {0x0600, 0x06FF, 5}, {0x0700, 0x074F, 6},
	{0x0900, 0x097F, 7}, {0x0980, 0x09FF, 8}, {0x0A00, 0x0A7F, 9},
	{0x0A80, 0x0AFF, 10}, {0x0B00, 0x0B7F, 11}, {0x0B80, 0x0BFF, 12},
	{0x0C00, 0x0C7F, 13}, {0x0C80, 0x0CFF, 14}, {0x0D00, 0x0D7F, 15},
	{0x0D80, 0x0DFF, 16}, {0x0E00, 0x0E7F, 17}, {0x0E80, 0x0EFF, 18},
	{0x0F00, 0x0FFF, 19}, {0x1000, 0x109F, 20}, {0x10A0, 0x10FF, 21},
	{0x1200, 0x137F, 22}, {0x13A0, 0x13FF, 23}, {0x3040, 0x30FF, 24},
	{0x3400, 0x4DBF, 25}, {0x4E00, 0x9FFF, 25}, {0xAC00, 0xD7AF, 26},
}

func scriptOf(s string) uint8 {
	for _, r := range s {
		if r < 0x0250 { // Latin, incl. ASCII and the Latin extensions
			continue
		}
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		for _, sr := range scriptRanges {
			if r >= sr.lo && r <= sr.hi {
				return sr.id
			}
		}
	}
	return 0
}

// countScripts reports how many distinct non-Latin writing systems appear among
// a place's alternate names.
//
// This is the prominence signal that survives where population does not:
// population is zero for ~91% of populated places, and a raw alternate-name
// count is confounded by transliteration variance — a Korean village can carry
// 67 romanisations of one name. Script diversity separates "the world names
// this place" from "this name is hard to spell in Latin".
func countScripts(alts []string) int {
	var seen uint32 // bitset over script ids 1..26
	for _, a := range alts {
		if id := scriptOf(a); id != 0 {
			seen |= 1 << id
		}
	}
	n := 0
	for ; seen != 0; seen &= seen - 1 {
		n++
	}
	return n
}
