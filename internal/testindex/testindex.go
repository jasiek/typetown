// Package testindex builds miniature but structurally complete indexes for
// tests, so a test that needs one does not have to assemble GeoNames inputs by
// hand. It is internal and exists only to be imported from tests.
package testindex

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasiek/typetown/internal/index"
)

// Place is one row of the fixture dump.
type Place struct {
	ID          string
	Name        string
	Lat, Lon    string
	FeatureCode string
	Country     string
	Admin1      string
	Population  string
}

// Default is a small set chosen to exercise the cases that matter: a dominant
// city against a tiny namesake, two same-named towns inside one country that
// only coordinates can separate, a city-state with no first-order division, and
// a name that is not plain ASCII.
var Default = []Place{
	{"2643743", "London", "51.50853", "-0.12574", "PPLC", "GB", "ENG", "8961989"},
	{"4298960", "London", "37.12898", "-84.08326", "PPLA2", "US", "KY", "8126"},
	{"3466512", "Londrina", "-23.31028", "-51.16278", "PPLA2", "BR", "18", "581382"},
	{"1880252", "Singapore", "1.28967", "103.85007", "PPLC", "SG", "00", "3547809"},
	{"2761369", "Wien", "48.20849", "16.37208", "PPLC", "AT", "09", "1691468"},
	{"2207266", "Springfield", "-43.31667", "172.16667", "PPL", "NZ", "E9", "200000"},
	{"4250542", "Springfield", "39.80172", "-89.64371", "PPL", "US", "IL", "100000"},
	{"4409896", "Springfield", "37.21533", "-93.29824", "PPL", "US", "MO", "170188"},
}

var admin1 = map[string]string{
	"GB.ENG": "England", "US.KY": "Kentucky", "BR.18": "Parana", "AT.09": "Vienna",
	"NZ.E9": "Canterbury", "US.IL": "Illinois", "US.MO": "Missouri",
}

var countries = map[string]string{
	"GB": "United Kingdom", "US": "United States", "BR": "Brazil",
	"SG": "Singapore", "AT": "Austria", "NZ": "New Zealand",
}

// Build writes the inputs and builds an index, returning its directory.
func Build(t *testing.T, places ...Place) string {
	t.Helper()
	if len(places) == 0 {
		places = Default
	}
	dir := t.TempDir()

	rows := make([]string, 0, len(places))
	for _, p := range places {
		cols := make([]string, 19)
		cols[0], cols[1], cols[2] = p.ID, p.Name, p.Name
		cols[4], cols[5] = p.Lat, p.Lon
		cols[6], cols[7] = "P", p.FeatureCode
		cols[8], cols[10] = p.Country, p.Admin1
		cols[14] = p.Population
		rows = append(rows, strings.Join(cols, "\t"))
	}
	dumpPath := filepath.Join(dir, "allCountries.zip")
	writeZip(t, dumpPath, "allCountries.txt", strings.Join(rows, "\n")+"\n")

	var a1 strings.Builder
	for code, name := range admin1 {
		a1.WriteString(code + "\t" + name + "\t" + name + "\t1\n")
	}
	admin1Path := filepath.Join(dir, "admin1CodesASCII.txt")
	write(t, admin1Path, a1.String())

	// The real countryInfo.txt opens with a comment block whose last line looks
	// like a header; the fixture keeps that shape so the parser is exercised.
	var ci strings.Builder
	ci.WriteString("# a comment preamble\n#ISO\tISO3\tISO-Numeric\tfips\tCountry\n")
	for code, name := range countries {
		ci.WriteString(code + "\t\t\t\t" + name + "\n")
	}
	countryPath := filepath.Join(dir, "countryInfo.txt")
	write(t, countryPath, ci.String())

	out := filepath.Join(dir, "index")
	if _, err := index.Build(index.BuildOptions{
		DumpPath: dumpPath, Admin1Path: admin1Path, CountryPath: countryPath, OutDir: out,
	}); err != nil {
		t.Fatalf("build test index: %v", err)
	}
	return out
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeZip(t *testing.T, path, member, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(member)
	if err != nil {
		t.Fatalf("create zip member: %v", err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("write zip member: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}
