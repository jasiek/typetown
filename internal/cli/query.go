package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/term"

	"typetown/internal/index"
)

func runQuery(env Env, args []string) error {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var (
		dir      = fs.String("index", "index", "index directory")
		limit    = fs.Int("limit", 10, "results to return")
		home     = fs.String("home", "", "ISO country code to bias results toward, e.g. US")
		asJSON   = fs.Bool("json", false, "emit results as JSON")
		pool     = fs.Int("pool", 2000, "candidates to consider before ranking")
		interact = fs.Bool("i", false, "interactive mode: read queries from stdin")
	)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "usage: typetown query [flags] <query>\n       typetown query -i [flags]\n\nlook up places by name prefix\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errParsed
	}

	ix, err := index.Open(*dir)
	if err != nil {
		return err
	}
	defer ix.Close()

	opts := index.SearchOptions{Limit: *limit, Home: strings.ToUpper(*home), Pool: *pool}

	if *interact {
		if fs.NArg() > 0 {
			return fmt.Errorf("query: -i takes no query argument")
		}
		// On a real terminal, search incrementally as the user types. When stdin
		// is a pipe or a file there are no keystrokes to react to, so fall back
		// to reading whole lines.
		if fd, ok := terminalFd(env.Stdin); ok {
			return liveSearch(env, ix, opts, *asJSON, fd)
		}
		return interactive(env, ix, opts, *asJSON)
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errParsed
	}
	return one(env, ix, strings.Join(fs.Args(), " "), opts, *asJSON, false)
}

func one(env Env, ix *index.Index, q string, opts index.SearchOptions, asJSON, timed bool) error {
	start := time.Now()
	res, err := ix.Search(q, opts)
	if err != nil {
		return err
	}
	took := time.Since(start)

	if asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	if len(res) == 0 {
		fmt.Fprintf(env.Stdout, "no matches for %q\n", q)
		return nil
	}
	for _, r := range res {
		fmt.Fprintf(env.Stdout, "  %-46s  %9.4f, %9.4f  %s\n", place(r), r.Lat, r.Lon, detail(r))
	}
	if timed {
		fmt.Fprintf(env.Stdout, "  (%d results in %s)\n", len(res), took.Round(time.Microsecond))
	}
	return nil
}

// place renders the locality the way a user reads it: town, region, country.
func place(r index.Result) string {
	parts := make([]string, 0, 3)
	for _, s := range []string{r.Name, r.Region, r.Country} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func detail(r index.Result) string {
	if r.Population > 0 {
		return fmt.Sprintf("pop %d, %s", r.Population, r.Kind)
	}
	return r.Kind
}

// terminalFd reports the file descriptor behind r when it is an interactive
// terminal. Env holds an io.Reader so tests can substitute a buffer, so the
// concrete file has to be recovered here.
func terminalFd(r io.Reader) (int, bool) {
	f, ok := r.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	fd := int(f.Fd())
	return fd, term.IsTerminal(fd)
}

// interactive is the line-based fallback for non-terminal input.
func interactive(env Env, ix *index.Index, opts index.SearchOptions, asJSON bool) error {
	m := ix.Manifest()
	fmt.Fprintf(env.Stdout, "typetown %s — %d places, %d keys. Type a place name; blank line or ctrl-d to quit.\n",
		Version(), m.Records, m.Keys)
	if opts.Home != "" {
		fmt.Fprintf(env.Stdout, "biasing results toward %s\n", opts.Home)
	}
	sc := bufio.NewScanner(env.Stdin)
	for {
		fmt.Fprint(env.Stdout, "> ")
		if !sc.Scan() {
			fmt.Fprintln(env.Stdout)
			return sc.Err()
		}
		q := strings.TrimSpace(sc.Text())
		if q == "" {
			return nil
		}
		if err := one(env, ix, q, opts, asJSON, true); err != nil {
			fmt.Fprintf(env.Stderr, "typetown: %v\n", err)
		}
	}
}
