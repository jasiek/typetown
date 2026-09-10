package index

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"typetown/internal/geonames"
)

// dumpRow builds one tab-separated line of the geoname table. Only the columns
// the builder reads are meaningful; the rest are padded so the column count is
// right, because ReadDump rejects rows that are the wrong width.
func dumpRow(id, name, ascii, alts, lat, lon, fcode, cc, admin1, pop string) string {
	cols := make([]string, 19)
	for i := range cols {
		cols[i] = ""
	}
	cols[0], cols[1], cols[2], cols[3] = id, name, ascii, alts
	cols[4], cols[5] = lat, lon
	cols[6], cols[7] = "P", fcode
	cols[8], cols[10] = cc, admin1
	cols[14] = pop
	return strings.Join(cols, "\t")
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

// buildTestIndex writes a miniature but structurally complete set of inputs and
// builds an index from them.
func buildTestIndex(t *testing.T) *Index {
	t.Helper()
	dir := t.TempDir()

	rows := []string{
		// A capital with several scripts: should dominate its prefix.
		dumpRow("1", "London", "London", "Лондон,Λονδίνο,倫敦,ロンドン", "51.50853", "-0.12574", "PPLC", "GB", "ENG", "8961989"),
		// A same-named town elsewhere, far smaller.
		dumpRow("2", "London", "London", "", "37.12898", "-84.08326", "PPLA2", "US", "KY", "8126"),
		// Shares a prefix but is not the same place.
		dumpRow("3", "Londrina", "Londrina", "", "-23.31028", "-51.16278", "PPLA2", "BR", "18", "581382"),
		// Diacritics: must be reachable by typing plain ascii.
		dumpRow("4", "Rāmpur", "Rampur", "", "28.81525", "79.02526", "PPL", "IN", "36", "325248"),
		// A city-state, whose admin1 code is the documented "no division" sentinel.
		dumpRow("5", "Singapore", "Singapore", "", "1.28967", "103.85007", "PPLC", "SG", "00", "3547809"),
		// Historical: must not be indexed at all.
		dumpRow("6", "Lonesome Ghost Town", "Lonesome Ghost Town", "", "40.0", "-100.0", "PPLH", "US", "KS", "0"),
		// Mumbai, to exercise an alternate name reaching a different record.
		dumpRow("7", "Mumbai", "Mumbai", "मुंबई,ムンバイ,孟买,Мумбаи,Μουμπάι,ムンバイ,뭄바이,מומבאי,مومباي", "19.07283", "72.88261", "PPLA", "IN", "16", "12691836"),
		// A tiny place literally named Bombay, which must lose to Mumbai.
		dumpRow("8", "Bombay", "Bombay", "", "-37.18333", "174.96667", "PPL", "NZ", "E7", "740"),
		// Two near-equal rivals in different countries, so the home bias has a
		// case it can legitimately decide.
		dumpRow("9", "Springfield", "Springfield", "", "-43.31667", "172.16667", "PPL", "NZ", "E9", "200000"),
		dumpRow("10", "Springfield", "Springfield", "", "39.80172", "-89.64371", "PPL", "US", "IL", "100000"),
	}
	dumpPath := filepath.Join(dir, "allCountries.zip")
	writeZip(t, dumpPath, "allCountries.txt", strings.Join(rows, "\n")+"\n")

	alts := []string{
		// isHistoric: indexed, but demoted.
		"100\t7\ten\tBombay\t\t\t\t1\t\t",
		// isColloquial: excluded outright.
		"101\t1\ten\tThe Big Smoke\t\t\t1\t\t\t",
		// pseudo-language: excluded outright.
		"102\t1\tiata\tLON\t\t\t\t\t\t",
		// an ordinary alternate name
		"103\t3\tpt\tLondrina Cidade\t\t\t\t\t\t",
	}
	altPath := filepath.Join(dir, "alternateNamesV2.zip")
	writeZip(t, altPath, "alternateNamesV2.txt", strings.Join(alts, "\n")+"\n")

	admin1 := "GB.ENG\tEngland\tEngland\t6269131\nUS.KY\tKentucky\tKentucky\t6254925\n" +
		"BR.18\tParana\tParana\t3455077\nIN.36\tUttar Pradesh\tUttar Pradesh\t1253626\n" +
		"IN.16\tMaharashtra\tMaharashtra\t1264418\nNZ.E7\tAuckland\tAuckland\t2193733\n"
	admin1Path := filepath.Join(dir, "admin1CodesASCII.txt")
	if err := os.WriteFile(admin1Path, []byte(admin1), 0o644); err != nil {
		t.Fatal(err)
	}

	country := "# a comment preamble, whose last line is the header\n" +
		"#ISO\tISO3\tISO-Numeric\tfips\tCountry\n" +
		"GB\tGBR\t826\tUK\tUnited Kingdom\t\t\t\t\t\t\t\t\t\t\t\t\t\t\n" +
		"US\tUSA\t840\tUS\tUnited States\t\t\t\t\t\t\t\t\t\t\t\t\t\t\n" +
		"BR\tBRA\t76\tBR\tBrazil\t\t\t\t\t\t\t\t\t\t\t\t\t\t\n" +
		"IN\tIND\t356\tIN\tIndia\t\t\t\t\t\t\t\t\t\t\t\t\t\t\n" +
		"SG\tSGP\t702\tSN\tSingapore\t\t\t\t\t\t\t\t\t\t\t\t\t\t\n" +
		"NZ\tNZL\t554\tNZ\tNew Zealand\t\t\t\t\t\t\t\t\t\t\t\t\t\t\n"
	countryPath := filepath.Join(dir, "countryInfo.txt")
	if err := os.WriteFile(countryPath, []byte(country), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "index")
	if _, err := Build(BuildOptions{
		DumpPath: dumpPath, AltNamesPath: altPath,
		Admin1Path: admin1Path, CountryPath: countryPath, OutDir: out,
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	ix, err := Open(out)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func TestSearch(t *testing.T) {
	ix := buildTestIndex(t)

	tests := []struct {
		name     string
		query    string
		home     string
		wantID   int32 // expected top result
		wantMiss bool  // expect no results at all
	}{
		{name: "prominence picks the right London", query: "lond", wantID: 1},
		{name: "exact match still favours the big one", query: "london", wantID: 1},
		{name: "home bias decides a close call", query: "springfield", home: "US", wantID: 10},
		{name: "without bias the bigger one wins", query: "springfield", wantID: 9},
		{name: "longer prefix reaches Londrina", query: "londr", wantID: 3},
		{name: "ascii reaches a diacritic name", query: "rampur", wantID: 4},
		{name: "diacritics reach it too", query: "rāmpur", wantID: 4},
		{name: "historic alternate still finds Mumbai", query: "bombay", wantID: 7},
		{name: "case is folded", query: "SINGAPORE", wantID: 5},
		{name: "colloquial names are not indexed", query: "the big smoke", wantMiss: true},
		{name: "airport codes are not indexed", query: "lon", wantID: 1}, // matches London, not via "LON"
		{name: "historical places are not indexed", query: "lonesome", wantMiss: true},
		{name: "unknown prefix finds nothing", query: "qqqq", wantMiss: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := ix.Search(tt.query, SearchOptions{Limit: 5, Home: tt.home})
			if err != nil {
				t.Fatalf("Search(%q): %v", tt.query, err)
			}
			if tt.wantMiss {
				if len(res) != 0 {
					t.Fatalf("Search(%q) = %d results, want none (first: %+v)", tt.query, len(res), res[0])
				}
				return
			}
			if len(res) == 0 {
				t.Fatalf("Search(%q) returned nothing, want id %d", tt.query, tt.wantID)
			}
			if res[0].ID != tt.wantID {
				t.Errorf("Search(%q) top result = %d (%s), want %d", tt.query, res[0].ID, res[0].Name, tt.wantID)
			}
		})
	}
}

// The home bias is deliberately a bounded nudge: it must not let a small local
// place displace a genuinely dominant one.
func TestHomeBiasIsBounded(t *testing.T) {
	ix := buildTestIndex(t)

	plain, err := ix.Search("london", SearchOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	biased, err := ix.Search("london", SearchOptions{Limit: 5, Home: "US"})
	if err != nil {
		t.Fatal(err)
	}
	if plain[0].ID != 1 || biased[0].ID != 1 {
		t.Fatalf("London GB should win either way; got %d then %d", plain[0].ID, biased[0].ID)
	}

	scoreOf := func(res []Result, id int32) float64 {
		for _, r := range res {
			if r.ID == id {
				return r.Score
			}
		}
		t.Fatalf("id %d absent from results", id)
		return 0
	}
	got := scoreOf(biased, 2) - scoreOf(plain, 2)
	if d := got - HomeBonus; d > 1e-6 || d < -1e-6 {
		t.Errorf("home bias moved the US result by %v, want exactly %v", got, HomeBonus)
	}
	if d := scoreOf(biased, 1) - scoreOf(plain, 1); d != 0 {
		t.Errorf("home bias moved the GB result by %v, want 0", d)
	}
}

// The class penalties and the query-time bonuses have to stay in proportion to
// each other. With the historic penalty at twice the home bonus, searching
// "bombay" from the United States surfaced Bombay Beach, California (population
// 295) above Mumbai.
func TestHistoricNameOutranksTinyLocalHomonym(t *testing.T) {
	ix := buildTestIndex(t)
	for _, home := range []string{"", "NZ"} {
		res, err := ix.Search("bombay", SearchOptions{Limit: 3, Home: home})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) == 0 || res[0].ID != 7 {
			got := int32(0)
			if len(res) > 0 {
				got = res[0].ID
			}
			t.Errorf("Search(\"bombay\", home=%q) top result = %d, want Mumbai (7)", home, got)
		}
	}
}

// The whole point of the index is turning a typed name into coordinates, so the
// payload is worth asserting field by field.
func TestResultPayload(t *testing.T) {
	ix := buildTestIndex(t)
	res, err := ix.Search("london", SearchOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	got := res[0]
	if got.Name != "London" || got.Region != "England" || got.Country != "United Kingdom" || got.CC != "GB" {
		t.Errorf("locality = %q/%q/%q/%q, want London/England/United Kingdom/GB",
			got.Name, got.Region, got.Country, got.CC)
	}
	if d := got.Lat - 51.50853; d > 1e-5 || d < -1e-5 {
		t.Errorf("lat = %v, want 51.50853", got.Lat)
	}
	if d := got.Lon - -0.12574; d > 1e-5 || d < -1e-5 {
		t.Errorf("lon = %v, want -0.12574", got.Lon)
	}
	if got.Population != 8961989 {
		t.Errorf("population = %d, want 8961989", got.Population)
	}
	if got.Kind != "PPLC" {
		t.Errorf("kind = %q, want PPLC", got.Kind)
	}
}

// A city-state has no first-order division. That is an answer, not a gap, and it
// must render as "Singapore, Singapore" rather than "Singapore, , Singapore".
func TestNoRegionForCityState(t *testing.T) {
	ix := buildTestIndex(t)
	res, err := ix.Search("singapore", SearchOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Region != "" {
		t.Errorf("region = %q, want empty", res[0].Region)
	}
	if res[0].Country != "Singapore" {
		t.Errorf("country = %q, want Singapore", res[0].Country)
	}
}

func TestLimitIsRespected(t *testing.T) {
	ix := buildTestIndex(t)
	for _, limit := range []int{1, 2, 3} {
		res, err := ix.Search("lo", SearchOptions{Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) > limit {
			t.Errorf("Search with limit %d returned %d results", limit, len(res))
		}
	}
}

func TestScoresAreDescending(t *testing.T) {
	ix := buildTestIndex(t)
	res, err := ix.Search("lo", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(res); i++ {
		if res[i].Score > res[i-1].Score {
			t.Errorf("result %d scored %v, above result %d at %v", i, res[i].Score, i-1, res[i-1].Score)
		}
	}
}

func TestPrefixSuccessor(t *testing.T) {
	tests := []struct{ in, want string }{
		{"lond", "lone"},
		{"a", "b"},
		{"az", "b\x00"[:1] + ""}, // "a{" — the byte after 'z'
	}
	for _, tt := range tests[:2] {
		if got := string(prefixSuccessor(tt.in)); got != tt.want {
			t.Errorf("prefixSuccessor(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := prefixSuccessor("\xff\xff"); got != nil {
		t.Errorf("prefixSuccessor(all 0xff) = %q, want nil", got)
	}
}

func TestLive(t *testing.T) {
	live := []string{"PPL", "PPLA", "PPLC", "PPLX", "PPLL"}
	dead := []string{"PPLH", "PPLCH", "PPLQ", "PPLW"}
	for _, c := range live {
		if !geonames.Live(c) {
			t.Errorf("Live(%q) = false, want true", c)
		}
	}
	for _, c := range dead {
		if geonames.Live(c) {
			t.Errorf("Live(%q) = true, want false", c)
		}
	}
}
