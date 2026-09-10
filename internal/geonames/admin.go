package geonames

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// adminNoCode is GeoNames' documented sentinel for "general features where no
// specific adm1 code is defined". It is a fact about the place (Singapore has no
// meaningful first-order division), not a lookup failure, so it is recognised
// here rather than being left to fail the join.
const adminNoCode = "00"

// Tables holds the two small lookup files that turn codes into display names.
// Both fit comfortably in memory: 3,865 and 252 rows.
type Tables struct {
	Admin1    map[string]string // "GB.ENG" -> "England"
	Countries map[string]string // "GB"     -> "United Kingdom"
}

// Region returns the display name of a place's first-order division, or "" when
// the place genuinely has none.
func (t Tables) Region(countryCode, admin1Code string) string {
	if admin1Code == "" || admin1Code == adminNoCode {
		return ""
	}
	return t.Admin1[countryCode+"."+admin1Code]
}

// Country returns the display name of a country, falling back to its code.
func (t Tables) Country(code string) string {
	if name, ok := t.Countries[code]; ok {
		return name
	}
	return code
}

// LoadTables reads admin1CodesASCII.txt and countryInfo.txt.
func LoadTables(admin1Path, countryPath string) (Tables, error) {
	t := Tables{Admin1: make(map[string]string, 4096), Countries: make(map[string]string, 256)}

	// admin1CodesASCII.txt: code, name, asciiname, geonameId
	if err := eachLine(admin1Path, func(line string) error {
		cols := strings.Split(line, "\t")
		if len(cols) < 3 {
			return fmt.Errorf("admin1: got %d columns, want >= 3", len(cols))
		}
		t.Admin1[cols[0]] = cols[1]
		return nil
	}); err != nil {
		return Tables{}, err
	}

	// countryInfo.txt carries a 50-line comment preamble whose *last* line is the
	// column header, so every '#' line is skipped and the first data line is data.
	if err := eachLine(countryPath, func(line string) error {
		if strings.HasPrefix(line, "#") {
			return nil
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 5 {
			return fmt.Errorf("countryInfo: got %d columns, want >= 5", len(cols))
		}
		t.Countries[cols[0]] = cols[4]
		return nil
	}); err != nil {
		return Tables{}, err
	}
	return t, nil
}

func eachLine(path string, fn func(string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if err := fn(line); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return sc.Err()
}
