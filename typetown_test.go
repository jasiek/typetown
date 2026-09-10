package typetown_test

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jasiek/typetown"
	"github.com/jasiek/typetown/internal/geonames"
	"github.com/jasiek/typetown/internal/index"
)

// dumpRow builds one line of the geoname table. Only the columns the builder
// reads carry meaning; the row must still be the right width.
func dumpRow(id, name, lat, lon, fcode, cc, admin1, pop string) string {
	cols := make([]string, 19)
	cols[0], cols[1], cols[2] = id, name, name
	cols[4], cols[5] = lat, lon
	cols[6], cols[7] = "P", fcode
	cols[8], cols[10] = cc, admin1
	cols[14] = pop
	return strings.Join(cols, "\t")
}

// buildFixture writes a miniature set of GeoNames inputs and builds an index.
func buildFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	rows := []string{
		dumpRow("2643743", "London", "51.50853", "-0.12574", "PPLC", "GB", "ENG", "8961989"),
		dumpRow("4298960", "London", "37.12898", "-84.08326", "PPLA2", "US", "KY", "8126"),
		dumpRow("3466512", "Londrina", "-23.31028", "-51.16278", "PPLA2", "BR", "18", "581382"),
		dumpRow("1880252", "Singapore", "1.28967", "103.85007", "PPLC", "SG", "00", "3547809"),
		dumpRow("2761369", "Wien", "48.20849", "16.37208", "PPLC", "AT", "09", "1691468"),
		// Two near-equal rivals, so the home bias has a case it can decide. It is
		// deliberately a nudge, not an override: it will not flip London.
		dumpRow("2207266", "Springfield", "-43.31667", "172.16667", "PPL", "NZ", "E9", "200000"),
		dumpRow("4250542", "Springfield", "39.80172", "-89.64371", "PPL", "US", "IL", "100000"),
		// A second American Springfield, larger than the first. Only coordinates
		// can choose between them; a country code cannot.
		dumpRow("4409896", "Springfield", "37.21533", "-93.29824", "PPL", "US", "MO", "170188"),
	}
	dumpPath := filepath.Join(dir, "allCountries.zip")
	f, err := os.Create(dumpPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("allCountries.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(strings.Join(rows, "\n") + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	admin1 := "GB.ENG\tEngland\tEngland\t1\nUS.KY\tKentucky\tKentucky\t2\n" +
		"BR.18\tParana\tParana\t3\nAT.09\tVienna\tVienna\t4\n" +
		"NZ.E9\tCanterbury\tCanterbury\t5\nUS.IL\tIllinois\tIllinois\t6\n" +
		"US.MO\tMissouri\tMissouri\t7\n"
	admin1Path := filepath.Join(dir, "admin1CodesASCII.txt")
	if err := os.WriteFile(admin1Path, []byte(admin1), 0o644); err != nil {
		t.Fatal(err)
	}
	country := "#header line\n" +
		"GB\t\t\t\tUnited Kingdom\nUS\t\t\t\tUnited States\nBR\t\t\t\tBrazil\n" +
		"SG\t\t\t\tSingapore\nAT\t\t\t\tAustria\nNZ\t\t\t\tNew Zealand\n"
	countryPath := filepath.Join(dir, "countryInfo.txt")
	if err := os.WriteFile(countryPath, []byte(country), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "index")
	if _, err := index.Build(index.BuildOptions{
		DumpPath: dumpPath, Admin1Path: admin1Path, CountryPath: countryPath,
		OutDir: out, Filter: geonames.Filter{},
	}); err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	return out
}

func openFixture(t *testing.T) *typetown.Index {
	t.Helper()
	ix, err := typetown.Open(buildFixture(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func TestSearch(t *testing.T) {
	ix := openFixture(t)

	res, err := ix.Search("lond", typetown.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 || res[0].ID != 2643743 {
		t.Fatalf("Search(lond) = %+v, want London GB first", res)
	}
	got := res[0]
	if got.Name != "London" || got.Region != "England" || got.Country != "United Kingdom" || got.CC != "GB" {
		t.Errorf("locality = %q/%q/%q/%q", got.Name, got.Region, got.Country, got.CC)
	}
	if d := got.Lat - 51.50853; d > 1e-5 || d < -1e-5 {
		t.Errorf("lat = %v, want 51.50853", got.Lat)
	}
}

// Diacritics are folded on both sides, so a caller typing plain ASCII reaches a
// name that is not plain ASCII.
func TestSearchFoldsDiacritics(t *testing.T) {
	ix := openFixture(t)
	for _, q := range []string{"wien", "WIEN", "Wień"} {
		res, err := ix.Search(q, typetown.SearchOptions{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(res) == 0 || res[0].ID != 2761369 {
			t.Errorf("Search(%q) did not reach Wien: %+v", q, res)
		}
	}
}

func TestSearchAfterCloseFails(t *testing.T) {
	ix, err := typetown.Open(buildFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Search("london", typetown.SearchOptions{}); err == nil {
		t.Error("Search on a closed index returned no error")
	}
	if err := ix.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestStats(t *testing.T) {
	s := openFixture(t).Stats()
	if s.Records != 8 {
		t.Errorf("Records = %d, want 8", s.Records)
	}
	if s.Version == 0 || s.Built == "" {
		t.Errorf("Stats looks unpopulated: %+v", s)
	}
}

func get(t *testing.T, h http.Handler, target string) (*http.Response, response) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	res := rec.Result()
	var body response
	if strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(res.Body).Decode(&body)
	}
	return res, body
}

// response mirrors the handler's JSON shape, so the test asserts on the wire
// format a caller actually receives rather than on internal types.
type response struct {
	Query   string `json:"query"`
	Results []struct {
		ID      int32   `json:"id"`
		Name    string  `json:"name"`
		Region  string  `json:"region"`
		Country string  `json:"country"`
		CC      string  `json:"cc"`
		Lat     float64 `json:"lat"`
		Lon     float64 `json:"lon"`
	} `json:"results"`
	Error string `json:"error"`
}

func TestHandler(t *testing.T) {
	h := typetown.Handler(openFixture(t))

	t.Run("returns the locality and coordinates", func(t *testing.T) {
		res, body := get(t, h, "/places?q=lond")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		if body.Query != "lond" {
			t.Errorf("query echoed as %q", body.Query)
		}
		if len(body.Results) == 0 {
			t.Fatal("no results")
		}
		first := body.Results[0]
		if first.ID != 2643743 || first.Country != "United Kingdom" || first.Region != "England" {
			t.Errorf("first result = %+v", first)
		}
		if first.Lat == 0 || first.Lon == 0 {
			t.Errorf("coordinates missing: %+v", first)
		}
	})

	t.Run("missing q is a client error", func(t *testing.T) {
		res, body := get(t, h, "/places")
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", res.StatusCode)
		}
		if body.Error == "" {
			t.Error("no error message in the body")
		}
	})

	t.Run("no match is an empty list, not an error", func(t *testing.T) {
		res, body := get(t, h, "/places?q=qqqqqq")
		if res.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", res.StatusCode)
		}
		if body.Results == nil {
			t.Error("results was null; want an empty list")
		}
		if len(body.Results) != 0 {
			t.Errorf("got %d results", len(body.Results))
		}
	})

	t.Run("limit is honoured and capped", func(t *testing.T) {
		_, body := get(t, h, "/places?q=lon&limit=1")
		if len(body.Results) != 1 {
			t.Errorf("limit=1 returned %d results", len(body.Results))
		}
		res, _ := get(t, h, "/places?q=lon&limit=0")
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=0 status = %d, want 400", res.StatusCode)
		}
		res, _ = get(t, h, "/places?q=lon&limit=abc")
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=abc status = %d, want 400", res.StatusCode)
		}
	})

	t.Run("rejects methods other than GET", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/places?q=lond", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("Allow = %q", got)
		}
	})

	t.Run("sets a cache header", func(t *testing.T) {
		res, _ := get(t, h, "/places?q=lond")
		if res.Header.Get("Cache-Control") == "" {
			t.Error("no Cache-Control on a successful response")
		}
	})
}

func TestHandlerHomeBias(t *testing.T) {
	ix := openFixture(t)

	// Springfield, New Zealand is marginally the larger of the two, so it leads
	// when nobody says where the caller is.
	_, plain := get(t, typetown.Handler(ix), "/?q=springfield")
	if len(plain.Results) == 0 || plain.Results[0].CC != "NZ" {
		t.Fatalf("without a home, first result was %+v", plain.Results)
	}

	// A request parameter, a configured default and a trusted header should all
	// reach the same place.
	_, byParam := get(t, typetown.Handler(ix), "/?q=springfield&home=us")
	if byParam.Results[0].CC != "US" {
		t.Errorf("home=us gave %s first", byParam.Results[0].CC)
	}

	_, byDefault := get(t, typetown.Handler(ix, typetown.WithHome("US")), "/?q=springfield")
	if byDefault.Results[0].CC != "US" {
		t.Errorf("WithHome(US) gave %s first", byDefault.Results[0].CC)
	}

	hdr := typetown.Handler(ix, typetown.WithHomeHeader("CF-IPCountry"))
	decode := func(country, query string) response {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, query, nil)
		if country != "" {
			req.Header.Set("CF-IPCountry", country)
		}
		hdr.ServeHTTP(rec, req)
		var body response
		_ = json.NewDecoder(rec.Body).Decode(&body)
		return body
	}
	if got := decode("US", "/?q=springfield").Results[0].CC; got != "US" {
		t.Errorf("CF-IPCountry: US gave %s first", got)
	}
	// An explicit parameter beats the header.
	if got := decode("US", "/?q=springfield&home=NZ").Results[0].CC; got != "NZ" {
		t.Errorf("home=NZ with CF-IPCountry: US gave %s first", got)
	}
	// "XX" is what proxies send when they could not resolve a country.
	if got := decode("XX", "/?q=springfield").Results[0].CC; got != "NZ" {
		t.Errorf("CF-IPCountry: XX gave %s first, want the unbiased answer", got)
	}
}

// The bias is a nudge, not an override: a local homonym must not displace a
// place that outranks it by orders of magnitude.
func TestHandlerHomeBiasDoesNotOverrideProminence(t *testing.T) {
	ix := openFixture(t)
	_, body := get(t, typetown.Handler(ix, typetown.WithHome("US")), "/?q=london")
	if len(body.Results) == 0 || body.Results[0].CC != "GB" {
		t.Errorf("London, Kentucky displaced London for a US caller: %+v", body.Results)
	}
}

func TestHandlerCORS(t *testing.T) {
	ix := openFixture(t)

	res, _ := get(t, typetown.Handler(ix), "/?q=lond")
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS headers sent without WithCORS")
	}

	h := typetown.Handler(ix, typetown.WithCORS("https://example.com"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?q=lond", nil)
	req.Header.Set("Origin", "https://example.com")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("allow-origin = %q", got)
	}

	// An origin that was not listed gets no header at all.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/?q=lond", nil)
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unlisted origin got allow-origin = %q", got)
	}
}

// The handler must be usable straight from a mux, at whatever path the caller
// chooses, which is the whole point of returning an http.Handler.
func TestHandlerMountsAnywhere(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/places", typetown.Handler(openFixture(t)))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/v1/places?q=singapore")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body response
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Results) == 0 || body.Results[0].Country != "Singapore" {
		t.Fatalf("got %+v", body.Results)
	}
	if body.Results[0].Region != "" {
		t.Errorf("city-state reported region %q, want empty", body.Results[0].Region)
	}
}

// Coordinates should pick out the nearer of two same-named places, which is the
// case a country code cannot decide.
func TestSearchNear(t *testing.T) {
	ix := openFixture(t)

	// Christchurch, New Zealand — a few km from Springfield, NZ.
	nzResults, err := ix.Search("springfield", typetown.SearchOptions{
		Limit: 2, Near: &typetown.LatLon{Lat: -43.53, Lon: 172.63},
	})
	if err != nil {
		t.Fatal(err)
	}
	if nzResults[0].CC != "NZ" {
		t.Errorf("from Christchurch, first result was %s", nzResults[0].CC)
	}

	// Chicago — a couple of hundred km from Springfield, Illinois.
	usResults, err := ix.Search("springfield", typetown.SearchOptions{
		Limit: 2, Near: &typetown.LatLon{Lat: 41.88, Lon: -87.63},
	})
	if err != nil {
		t.Fatal(err)
	}
	if usResults[0].CC != "US" {
		t.Errorf("from Chicago, first result was %s", usResults[0].CC)
	}
}

// Coordinates decide between two places in one country, which a country code
// cannot. Missouri's Springfield is the larger, so it leads until the caller's
// position says otherwise.
func TestSearchNearSeparatesPlacesWithinACountry(t *testing.T) {
	ix := openFixture(t)

	plain, err := ix.Search("springfield", typetown.SearchOptions{Limit: 3, Home: "US"})
	if err != nil {
		t.Fatal(err)
	}
	if plain[0].Region != "Missouri" {
		t.Fatalf("with only a country, first result was %s", plain[0].Region)
	}

	local, err := ix.Search("springfield", typetown.SearchOptions{
		Limit: 3, Home: "US", Near: &typetown.LatLon{Lat: 39.80, Lon: -89.64},
	})
	if err != nil {
		t.Fatal(err)
	}
	if local[0].Region != "Illinois" {
		t.Errorf("standing in Springfield, Illinois, first result was %s", local[0].Region)
	}
}

// The two hints combine by taking whichever helps more, so adding one can never
// make a result score lower than it would have without it.
func TestHintsAreMonotone(t *testing.T) {
	ix := openFixture(t)
	scoreOf := func(opts typetown.SearchOptions) map[int32]float64 {
		t.Helper()
		res, err := ix.Search("springfield", opts)
		if err != nil {
			t.Fatal(err)
		}
		m := map[int32]float64{}
		for _, r := range res {
			m[r.ID] = r.Score
		}
		return m
	}
	none := scoreOf(typetown.SearchOptions{Limit: 5})
	home := scoreOf(typetown.SearchOptions{Limit: 5, Home: "US"})
	both := scoreOf(typetown.SearchOptions{Limit: 5, Home: "US",
		Near: &typetown.LatLon{Lat: 41.88, Lon: -87.63}})

	for id, base := range none {
		if h, ok := home[id]; ok && h < base-1e-9 {
			t.Errorf("record %d scored %v with a country hint, below %v without", id, h, base)
		}
		if b, ok := both[id]; ok {
			if h, ok := home[id]; ok && b < h-1e-9 {
				t.Errorf("record %d scored %v with a position added, below %v with the country alone", id, b, h)
			}
		}
	}
}

// Proximity is bounded like the country bias: standing in a small town does not
// make it outrank a capital.
func TestSearchNearIsBounded(t *testing.T) {
	ix := openFixture(t)
	res, err := ix.Search("london", typetown.SearchOptions{
		Limit: 2,
		Near:  &typetown.LatLon{Lat: 37.13, Lon: -84.08}, // London, Kentucky itself
	})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].CC != "GB" {
		t.Errorf("standing in London, Kentucky displaced London: %+v", res)
	}
}

func TestHandlerNear(t *testing.T) {
	h := typetown.Handler(openFixture(t))

	_, nz := get(t, h, "/?q=springfield&lat=-43.53&lon=172.63")
	if len(nz.Results) == 0 || nz.Results[0].CC != "NZ" {
		t.Errorf("lat/lon near Christchurch gave %+v", nz.Results)
	}
	_, us := get(t, h, "/?q=springfield&lat=41.88&lon=-87.63")
	if len(us.Results) == 0 || us.Results[0].CC != "US" {
		t.Errorf("lat/lon near Chicago gave %+v", us.Results)
	}

	// lat and lon are a pair, and out-of-range values are a mistake, not a
	// silently-ignored hint.
	for _, bad := range []string{
		"/?q=lond&lat=51.5",
		"/?q=lond&lon=-0.12",
		"/?q=lond&lat=abc&lon=-0.12",
		"/?q=lond&lat=91&lon=0",
		"/?q=lond&lat=0&lon=181",
	} {
		res, body := get(t, h, bad)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, res.StatusCode)
		}
		if body.Error == "" {
			t.Errorf("%s: no error message", bad)
		}
	}
}

// Search is documented as safe for concurrent use, which is the whole basis for
// handing the index to net/http. Under -race this is what proves it.
func TestSearchIsConcurrencySafe(t *testing.T) {
	ix := openFixture(t)

	queries := []string{"lond", "springfield", "wien", "singapore", "londr", "s", "zzz"}
	var wg sync.WaitGroup
	errs := make(chan error, len(queries)*8)

	for i := 0; i < 8; i++ {
		for _, q := range queries {
			wg.Add(1)
			go func(q string) {
				defer wg.Done()
				for n := 0; n < 25; n++ {
					opts := typetown.SearchOptions{Limit: 5, Home: "US"}
					if n%2 == 0 {
						opts.Near = &typetown.LatLon{Lat: 51.5, Lon: -0.12}
					}
					res, err := ix.Search(q, opts)
					if err != nil {
						errs <- err
						return
					}
					// Results must stay internally consistent under concurrency.
					for j := 1; j < len(res); j++ {
						if res[j].Score > res[j-1].Score {
							errs <- fmt.Errorf("query %q: results came back out of order", q)
							return
						}
					}
				}
			}(q)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// The handler is held by a mux and called from many connections at once.
func TestHandlerIsConcurrencySafe(t *testing.T) {
	srv := httptest.NewServer(typetown.Handler(openFixture(t), typetown.WithHome("GB")))
	defer srv.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			url := srv.URL + "/?q=lond"
			if i%2 == 0 {
				url += "&lat=51.5&lon=-0.12"
			}
			res, err := http.Get(url)
			if err != nil {
				errs <- err
				return
			}
			defer res.Body.Close()
			var body response
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				errs <- err
				return
			}
			if len(body.Results) == 0 {
				errs <- fmt.Errorf("request %d came back empty", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
