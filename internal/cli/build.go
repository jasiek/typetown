package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jasiek/typetown/internal/geonames"
	"github.com/jasiek/typetown/internal/index"
)

func runBuild(env Env, args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var (
		sources   = fs.String("sources", "sources", "directory holding the GeoNames inputs")
		out       = fs.String("out", "index", "directory to write the index into")
		countries = fs.String("countries", "", "comma-separated ISO country codes to include (default: all)")
		minPop    = fs.Int64("min-population", 0, "skip places below this population")
		parts     = fs.Bool("include-parts", false, "include PPLX, sections of a populated place")
		noAlts    = fs.Bool("no-alt-names", false, "skip alternateNamesV2.zip, indexing only primary names")
		quiet     = fs.Bool("quiet", false, "suppress progress output")
	)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "usage: typetown build [flags]\n\nbuild an autocomplete index from the GeoNames inputs\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errParsed
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("build: unexpected argument %q", fs.Arg(0))
	}

	opts := index.BuildOptions{
		DumpPath:    filepath.Join(*sources, "allCountries.zip"),
		Admin1Path:  filepath.Join(*sources, "admin1CodesASCII.txt"),
		CountryPath: filepath.Join(*sources, "countryInfo.txt"),
		OutDir:      *out,
		Filter: geonames.Filter{
			MinPopulation: *minPop,
			IncludeParts:  *parts,
			Countries:     parseCountries(*countries),
		},
	}
	if !*noAlts {
		alt := filepath.Join(*sources, "alternateNamesV2.zip")
		if _, err := os.Stat(alt); err == nil {
			opts.AltNamesPath = alt
		} else {
			fmt.Fprintf(env.Stderr, "typetown: %s not found, building without alternate names\n", alt)
		}
	}
	if !*quiet {
		start := time.Now()
		opts.Progress = func(stage string, n int) {
			fmt.Fprintf(env.Stderr, "  %-12s %10d  (%s)\n", stage, n, time.Since(start).Round(time.Millisecond))
		}
	}

	start := time.Now()
	m, err := index.Build(opts)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)

	fmt.Fprintf(env.Stdout, "built %s in %s\n", *out, elapsed.Round(time.Millisecond))
	fmt.Fprintf(env.Stdout, "  records  %10d\n", m.Records)
	fmt.Fprintf(env.Stdout, "  keys     %10d\n", m.Keys)
	fmt.Fprintf(env.Stdout, "  postings %10d\n", m.Postings)
	fmt.Fprintf(env.Stdout, "  filter   %s\n", m.Filter)
	if sizes, err := dirSizes(*out); err == nil {
		fmt.Fprintf(env.Stdout, "  on disk  %s\n", sizes)
	}
	return nil
}

func parseCountries(s string) map[string]bool {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	m := map[string]bool{}
	for _, c := range strings.Split(s, ",") {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			m[c] = true
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func dirSizes(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var parts []string
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
		parts = append(parts, fmt.Sprintf("%s %s", e.Name(), humanBytes(info.Size())))
	}
	return fmt.Sprintf("%s total  (%s)", humanBytes(total), strings.Join(parts, ", ")), nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
