package geonames

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Column offsets in the main geoname table. The dump has no header row, so
// these are the schema; see https://download.geonames.org/export/dump/readme.txt
const (
	colID = iota
	colName
	colASCIIName
	colAltNames
	colLat
	colLon
	colFeatureClass
	colFeatureCode
	colCountry
	colCC2
	colAdmin1
	colAdmin2
	colAdmin3
	colAdmin4
	colPopulation
	colElevation
	colDEM
	colTimezone
	colModified
	numColumns
)

// Filter narrows which rows become records.
type Filter struct {
	Countries     map[string]bool // nil means every country
	MinPopulation int64
	IncludeParts  bool // keep PPLX, sections of a populated place
}

func (f Filter) keep(r *Record) bool {
	if !Live(r.FeatureCode) {
		return false
	}
	if !f.IncludeParts && r.FeatureCode == "PPLX" {
		return false
	}
	if r.Population < f.MinPopulation {
		return false
	}
	if f.Countries != nil && !f.Countries[r.CountryCode] {
		return false
	}
	return true
}

// ReadDump streams populated places out of allCountries.zip, calling fn for each
// row that passes the filter. The dump is 1.8 GB uncompressed and 13.5M rows, so
// nothing is buffered: fn must copy anything it wants to keep.
func ReadDump(path string, f Filter, fn func(*Record) error) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open dump: %w", err)
	}
	defer zr.Close()

	var member *zip.File
	for _, file := range zr.File {
		if strings.HasSuffix(file.Name, ".txt") {
			member = file
			break
		}
	}
	if member == nil {
		return fmt.Errorf("open dump: %s contains no .txt member", path)
	}
	rc, err := member.Open()
	if err != nil {
		return fmt.Errorf("open dump member: %w", err)
	}
	defer rc.Close()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // alternatenames can be long
	line := 0
	for sc.Scan() {
		line++
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) != numColumns {
			return fmt.Errorf("dump line %d: got %d columns, want %d", line, len(cols), numColumns)
		}
		if cols[colFeatureClass] != "P" {
			continue
		}
		rec := Record{
			Name:        cols[colName],
			ASCIIName:   cols[colASCIIName],
			CountryCode: cols[colCountry],
			Admin1Code:  cols[colAdmin1],
			FeatureCode: cols[colFeatureCode],
		}
		id, err := strconv.ParseInt(cols[colID], 10, 32)
		if err != nil {
			return fmt.Errorf("dump line %d: bad geonameid %q", line, cols[colID])
		}
		rec.ID = int32(id)
		rec.Lat, _ = strconv.ParseFloat(cols[colLat], 64)
		rec.Lon, _ = strconv.ParseFloat(cols[colLon], 64)
		rec.Population, _ = strconv.ParseInt(cols[colPopulation], 10, 64)

		if !f.keep(&rec) {
			continue
		}
		if err := fn(&rec); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("read dump: %w", err)
	}
	return nil
}
