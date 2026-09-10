package cli

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jasiek/typetown/internal/index"
)

// benchCase is one line of a query set. The expected id is optional: without it
// the case measures latency only, with it the case also measures accuracy.
type benchCase struct {
	query  string
	want   int32         // 0 means "no expectation"
	weight int           // how much this query matters; defaults to 1
	home   string        // country to bias toward for this case; "" uses the -home flag
	near   *index.LatLon // where the caller is; supersedes home when set
}

func runBench(env Env, args []string) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var (
		dir        = fs.String("index", "index", "index directory")
		set        = fs.String("queries", "", "query set file (default: a built-in sample)")
		limit      = fs.Int("limit", 10, "results to request per query")
		home       = fs.String("home", "", "ISO country code to bias results toward")
		pool       = fs.Int("pool", 2000, "candidates to consider before ranking")
		repeat     = fs.Int("repeat", 1, "run the whole query set this many times")
		prefixes   = fs.String("prefixes", "", "also run truncations of each query at these lengths, e.g. 3,4,5")
		ignoreNear = fs.Bool("ignore-near", false, "ignore any lat/lon in the query set, to measure what they are worth")
	)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, `usage: typetown bench [flags]

Benchmark latency, and accuracy when the query set carries expectations.

A query set is one case per line:
    <query>
    <query>	<expected geonameid>
    <query>	<expected geonameid>	<weight>
    <query>	<expected geonameid>	<weight>	<home country>
    <query>	<expected geonameid>	<weight>	<home country>	<lat>	<lon>

A per-case home country overrides -home, so one query set can model traffic
from several countries at their real proportions. A per-case lat and lon model
a caller whose position is known, and supersede the country.

flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errParsed
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("bench: unexpected argument %q", fs.Arg(0))
	}

	cases, err := loadQuerySet(*set)
	if err != nil {
		return err
	}
	if lens := parseLengths(*prefixes); len(lens) > 0 {
		cases = expandPrefixes(cases, lens)
	}
	if len(cases) == 0 {
		return fmt.Errorf("bench: query set is empty")
	}

	ix, err := index.Open(*dir)
	if err != nil {
		return err
	}
	defer ix.Close()
	opts := index.SearchOptions{Limit: *limit, Home: strings.ToUpper(*home), Pool: *pool}

	// tally accumulates weighted hit counts, for the whole set and per country.
	type tally struct{ total, h1, h3, h10 int }
	var (
		lat       []time.Duration
		graded    int
		all       tally
		byCountry = map[string]*tally{}
		misses    []benchCase
		emptyRes  int
	)
	for r := 0; r < *repeat; r++ {
		for _, c := range cases {
			o := opts
			if c.home != "" {
				o.Home = c.home // the case knows where its user is
			}
			if c.near != nil && !*ignoreNear {
				o.Near = c.near
			}
			start := time.Now()
			res, err := ix.Search(c.query, o)
			lat = append(lat, time.Since(start))
			if err != nil {
				return err
			}
			if len(res) == 0 {
				emptyRes++
			}
			if r > 0 || c.want == 0 {
				continue
			}
			graded++
			rank := 0
			for i, x := range res {
				if x.ID == c.want {
					rank = i + 1
					break
				}
			}
			key := c.home
			if key == "" {
				key = "—"
			}
			t := byCountry[key]
			if t == nil {
				t = &tally{}
				byCountry[key] = t
			}
			for _, dst := range []*tally{&all, t} {
				dst.total += c.weight
				if rank == 1 {
					dst.h1 += c.weight
				}
				if rank > 0 && rank <= 3 {
					dst.h3 += c.weight
				}
				if rank > 0 && rank <= *limit {
					dst.h10 += c.weight
				}
			}
			if rank == 0 {
				misses = append(misses, c)
			}
		}
	}

	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	total := time.Duration(0)
	for _, d := range lat {
		total += d
	}
	fmt.Fprintf(env.Stdout, "queries      %d (%d cases x %d)\n", len(lat), len(cases), *repeat)
	fmt.Fprintf(env.Stdout, "empty result %d\n", emptyRes)
	fmt.Fprintf(env.Stdout, "throughput   %.0f queries/sec\n", float64(len(lat))/total.Seconds())
	fmt.Fprintf(env.Stdout, "latency      mean %s  p50 %s  p90 %s  p99 %s  max %s\n",
		round(total/time.Duration(len(lat))), round(pct(lat, 50)), round(pct(lat, 90)),
		round(pct(lat, 99)), round(lat[len(lat)-1]))
	if graded > 0 {
		pc := func(n, d int) float64 {
			if d == 0 {
				return 0
			}
			return 100 * float64(n) / float64(d)
		}
		fmt.Fprintf(env.Stdout, "\naccuracy over %d graded cases (weighted)\n", graded)
		fmt.Fprintf(env.Stdout, "  rank 1     %5.1f%%\n", pc(all.h1, all.total))
		fmt.Fprintf(env.Stdout, "  top 3      %5.1f%%\n", pc(all.h3, all.total))
		fmt.Fprintf(env.Stdout, "  top %-2d     %5.1f%%\n", *limit, pc(all.h10, all.total))

		if len(byCountry) > 1 {
			keys := make([]string, 0, len(byCountry))
			for k := range byCountry {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				return byCountry[keys[i]].total > byCountry[keys[j]].total
			})
			fmt.Fprintf(env.Stdout, "\n  by home country, heaviest first\n")
			fmt.Fprintf(env.Stdout, "  %-6s %7s %8s %8s %8s\n", "home", "share", "rank 1", "top 3", "top "+strconv.Itoa(*limit))
			for _, k := range keys {
				t := byCountry[k]
				fmt.Fprintf(env.Stdout, "  %-6s %6.1f%% %7.1f%% %7.1f%% %7.1f%%\n",
					k, pc(t.total, all.total), pc(t.h1, t.total), pc(t.h3, t.total), pc(t.h10, t.total))
			}
		}
		if len(misses) > 0 {
			fmt.Fprintf(env.Stdout, "\n  %d cases outside the top %d, worst-weighted first:\n", len(misses), *limit)
			sort.Slice(misses, func(i, j int) bool { return misses[i].weight > misses[j].weight })
			for i, m := range misses {
				if i >= 10 {
					fmt.Fprintf(env.Stdout, "    ... and %d more\n", len(misses)-i)
					break
				}
				fmt.Fprintf(env.Stdout, "    %-24q want id %d (weight %d)\n", m.query, m.want, m.weight)
			}
		}
	}
	return nil
}

func pct(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := (len(sorted)*p + 99) / 100
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func round(d time.Duration) time.Duration {
	if d < time.Microsecond {
		return d
	}
	if d < time.Millisecond {
		return d.Round(time.Microsecond / 10)
	}
	return d.Round(time.Microsecond)
}

// defaultQuerySet exercises the shapes that matter: a short ambiguous prefix, a
// name reached through an alternate, a name with diacritics, and a homonym.
var defaultQuerySet = []string{
	"lond", "paris", "san fr", "new york", "tokyo", "mumbai", "bombay",
	"zurich", "koln", "sao paulo", "moscow", "delhi", "york", "springfield",
	"las vegas", "provo", "reno", "austin", "z", "a", "st", "san", "los ang",
}

func loadQuerySet(path string) ([]benchCase, error) {
	if path == "" {
		cases := make([]benchCase, 0, len(defaultQuerySet))
		for _, q := range defaultQuerySet {
			cases = append(cases, benchCase{query: q, weight: 1})
		}
		return cases, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open query set: %w", err)
	}
	defer f.Close()

	var cases []benchCase
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		cols := strings.Split(text, "\t")
		c := benchCase{query: cols[0], weight: 1}
		if len(cols) > 1 && cols[1] != "" {
			id, err := strconv.ParseInt(cols[1], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("query set line %d: bad geonameid %q", line, cols[1])
			}
			c.want = int32(id)
		}
		if len(cols) > 2 && cols[2] != "" {
			w, err := strconv.Atoi(cols[2])
			if err != nil {
				return nil, fmt.Errorf("query set line %d: bad weight %q", line, cols[2])
			}
			c.weight = w
		}
		if len(cols) > 3 && cols[3] != "" {
			c.home = strings.ToUpper(strings.TrimSpace(cols[3]))
		}
		if len(cols) > 5 && cols[4] != "" && cols[5] != "" {
			lat, errLat := strconv.ParseFloat(cols[4], 64)
			lon, errLon := strconv.ParseFloat(cols[5], 64)
			if errLat != nil || errLon != nil {
				return nil, fmt.Errorf("query set line %d: bad lat/lon %q,%q", line, cols[4], cols[5])
			}
			c.near = &index.LatLon{Lat: lat, Lon: lon}
		}
		cases = append(cases, c)
	}
	return cases, sc.Err()
}

func parseLengths(s string) []int {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// expandPrefixes turns each case into several, one per truncation length, so a
// single query set can measure how accuracy improves as a user types.
func expandPrefixes(cases []benchCase, lens []int) []benchCase {
	out := make([]benchCase, 0, len(cases)*(len(lens)+1))
	for _, c := range cases {
		out = append(out, c)
		for _, n := range lens {
			if n < len(c.query) {
				short := c
				short.query = c.query[:n]
				out = append(out, short)
			}
		}
	}
	return out
}
