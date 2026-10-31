package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountriesRestrictResults(t *testing.T) {
	ix := buildTestIndex(t)
	tests := []struct {
		query     string
		countries []string
		wantIDs   []int32
	}{
		{"london", []string{"US"}, []int32{2}},
		{"london", []string{"gb"}, []int32{1}}, // case does not matter
		{"london", []string{"GB", "US"}, []int32{1, 2}},
		{"springfield", []string{"NZ"}, []int32{9}},
		{"london", []string{"ZZ"}, nil}, // no such country: nothing, not everything
		{"london", []string{"ZZ", "US"}, []int32{2}},
		{"london", []string{"US", "us"}, []int32{2}}, // duplicates are harmless
		{"lo", []string{"IN"}, nil},
	}
	for _, tt := range tests {
		res, err := ix.Search(tt.query, SearchOptions{Limit: 10, Countries: tt.countries})
		if err != nil {
			t.Fatalf("Search(%q, %v): %v", tt.query, tt.countries, err)
		}
		var got []int32
		for _, r := range res {
			got = append(got, r.ID)
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.wantIDs) {
			t.Errorf("Search(%q, %v) = %v, want %v", tt.query, tt.countries, got, tt.wantIDs)
		}
	}
}

// buildCountryIndex builds enough places under one prefix that a country's
// shortlist fills up and has to leave some out: 300 per country against a
// shortlist of hotK. Populations rise with the ordinal, so which places a
// shortlist keeps is predictable.
func buildCountryIndex(t *testing.T, threshold int) *Index {
	t.Helper()
	dir := t.TempDir()
	var rows []string
	for i := 0; i < 600; i++ {
		cc := []string{"US", "PL"}[i%2]
		name := "Lo" + string(rune('a'+i/26%26)) + string(rune('a'+i%26))
		lat := fmt.Sprintf("%.5f", 30+float64(i)*0.05)
		lon := fmt.Sprintf("%.5f", -100+float64(i)*0.05)
		rows = append(rows, dumpRow(fmt.Sprint(i+1), name, name, "", lat, lon, "PPL", cc, "", fmt.Sprint(i*10)))
	}
	// Places under another prefix, which no "lo" query may return.
	rows = append(rows,
		dumpRow("1001", "Madrid", "Madrid", "", "40.4", "-3.7", "PPLC", "ES", "", "3255944"),
		dumpRow("1002", "Mobile", "Mobile", "", "30.7", "-88.0", "PPL", "US", "", "187041"),
	)
	dumpPath := filepath.Join(dir, "allCountries.zip")
	writeZip(t, dumpPath, "allCountries.txt", strings.Join(rows, "\n")+"\n")

	admin1Path := filepath.Join(dir, "admin1CodesASCII.txt")
	if err := os.WriteFile(admin1Path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	countryPath := filepath.Join(dir, "countryInfo.txt")
	country := "#header\nUS\t\t\t\tUnited States\nPL\t\t\t\tPoland\nES\t\t\t\tSpain\n"
	if err := os.WriteFile(countryPath, []byte(country), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "index")
	m, err := Build(BuildOptions{
		DumpPath: dumpPath, Admin1Path: admin1Path, CountryPath: countryPath,
		OutDir: out, HotThreshold: threshold,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Logf("threshold %d -> %d hot prefixes, %d per country", threshold, m.HotPrefixes, m.CountryHotPrefixes)
	ix, err := Open(out)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

// A restricted search must return exactly the best matches in those countries,
// whether a country's shortlist answered it or its whole range was walked. The
// same places are built twice, once with shortlists and once without, and every
// combination of limit, home and position must agree — including a limit past
// what a shortlist holds, and a position beside a place the shortlist left out.
func TestCountryShortlistsAgreeWithWalking(t *testing.T) {
	short := buildCountryIndex(t, 2)
	walked := buildCountryIndex(t, 1_000_000)
	if short.Manifest().CountryHotPrefixes == 0 {
		t.Fatal("low threshold produced no per-country shortlists")
	}
	if n := walked.Manifest().CountryHotPrefixes; n != 0 {
		t.Fatalf("high threshold produced %d per-country shortlists, want 0", n)
	}

	// Place 81 is in the US, and its population of 800 leaves it off the US
	// shortlist for "lo", which keeps only the 256 most populous. Standing on it
	// has to bring it back.
	beside := &LatLon{Lat: 34.0, Lon: -96.0}

	for _, q := range []string{"l", "lo", "loa", "lob", "lobc", "m"} {
		for _, countries := range [][]string{{"US"}, {"PL"}, {"US", "PL"}, {"PL", "ES"}} {
			for _, limit := range []int{1, 10, 300, 1000} {
				for _, home := range []string{"", "US", "PL"} {
					for _, near := range []*LatLon{nil, beside} {
						opts := SearchOptions{Limit: limit, Home: home, Near: near, Countries: countries}
						a, err := short.Search(q, opts)
						if err != nil {
							t.Fatal(err)
						}
						b, err := walked.Search(q, opts)
						if err != nil {
							t.Fatal(err)
						}
						where := fmt.Sprintf("query %q countries %v limit %d home %q near %v", q, countries, limit, home, near != nil)
						if len(a) != len(b) {
							t.Errorf("%s: shortlisted returned %d results, walked %d", where, len(a), len(b))
							continue
						}
						for i := range a {
							if a[i].ID != b[i].ID || a[i].Score != b[i].Score {
								t.Errorf("%s result %d: shortlisted %d@%v, walked %d@%v",
									where, i, a[i].ID, a[i].Score, b[i].ID, b[i].Score)
								break
							}
						}
					}
				}
			}
		}
	}

	res, err := short.Search("lo", SearchOptions{Limit: 1, Near: beside, Countries: []string{"US"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 || res[0].ID != 81 {
		t.Errorf("standing beside place 81, top result = %+v, want 81", res)
	}
}

// Every match means every match: a limit beyond what any shortlist holds must
// return each place in the country under the prefix, and nothing from elsewhere.
func TestCountriesReturnEveryMatch(t *testing.T) {
	ix := buildCountryIndex(t, 2)
	res, err := ix.Search("lo", SearchOptions{Limit: 1000, Countries: []string{"US"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 300 {
		t.Fatalf("got %d results, want all 300 US places under %q", len(res), "lo")
	}
	for _, r := range res {
		if r.CC != "US" {
			t.Fatalf("result %d (%s) is in %s, not the US", r.ID, r.Name, r.CC)
		}
	}
}
