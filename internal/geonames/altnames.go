package geonames

import (
	"archive/zip"
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// Columns of the alternate names table.
const (
	altColID = iota
	altColGeonameID
	altColLanguage
	altColName
	altColPreferred
	altColShort
	altColColloquial
	altColHistoric
	altColFrom
	altColTo
)

// pseudoLanguages are isolanguage values that do not denote a language at all:
// links, identifiers and transport codes. They are 9.0% of alternate names for
// populated places and are pure noise in a name index — "ZRG" is Bratislava's
// airport, not something a person types when looking for the city.
var pseudoLanguages = map[string]bool{
	"iata": true, "icao": true, "faac": true, "unlc": true,
	"post": true, "link": true, "wkdt": true, "fr_1793": true,
	"phon": true, "piny": true, "abbr": true,
}

// AltName is one alternate name, already classified.
type AltName struct {
	GeonameID int32
	Name      string
	Class     Class
}

// ReadAltNames streams alternateNamesV2.zip, calling fn for every name that
// belongs to a wanted place and survives filtering.
//
// The equivalent column in allCountries.txt is a lossy convenience copy with the
// flags stripped, which is why this file is worth its 204 MB: without
// isColloquial, "New York Van Java" is an alternate name of Jakarta and outranks
// New York City for the query "new y".
func ReadAltNames(path string, wanted func(int32) bool, official func(int32, string) bool, fn func(AltName) error) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open alt names: %w", err)
	}
	defer zr.Close()

	var member *zip.File
	for _, file := range zr.File {
		if strings.HasPrefix(file.Name, "alternateNames") && strings.HasSuffix(file.Name, ".txt") {
			member = file
			break
		}
	}
	if member == nil {
		return fmt.Errorf("open alt names: %s contains no alternateNames*.txt member", path)
	}
	rc, err := member.Open()
	if err != nil {
		return fmt.Errorf("open alt names member: %w", err)
	}
	defer rc.Close()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < altColHistoric+1 {
			continue // trailing from/to columns are optional, earlier ones are not
		}
		if cols[altColColloquial] == "1" {
			continue // slang: "Frisco", "Big Apple", "New York Van Java"
		}
		lang := cols[altColLanguage]
		if pseudoLanguages[lang] {
			continue
		}
		name := cols[altColName]
		if name == "" {
			continue
		}
		id, err := strconv.ParseInt(cols[altColGeonameID], 10, 32)
		if err != nil {
			continue
		}
		gid := int32(id)
		if !wanted(gid) {
			continue
		}

		// isHistoric demotes rather than excludes: "Bombay" is both historic and
		// the preferred name for Mumbai in Spanish and French, and people still
		// type it.
		class := ClassAlternate
		switch {
		case cols[altColHistoric] == "1":
			class = ClassHistoric
		case cols[altColPreferred] == "1", cols[altColShort] == "1", official(gid, lang):
			class = ClassOfficial
		}
		if err := fn(AltName{GeonameID: gid, Name: name, Class: class}); err != nil {
			return err
		}
	}
	return sc.Err()
}
