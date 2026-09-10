package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jasiek/typetown"
)

func runServe(env Env, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	var (
		dir        = fs.String("index", "index", "index directory")
		addr       = fs.String("addr", "localhost:8080", "address to listen on; use :8080 to accept from anywhere")
		path       = fs.String("path", "/places", "path to serve lookups on")
		home       = fs.String("home", "", "ISO country code to bias results toward by default")
		homeHeader = fs.String("home-header", "", "read the caller's country from this header, e.g. CF-IPCountry")
		limit      = fs.Int("limit", 10, "results returned when a request does not ask")
		maxLimit   = fs.Int("max-limit", 50, "most results a request may ask for")
		cors       = fs.String("cors", "", "comma-separated origins allowed to call this, or * for any")
		quiet      = fs.Bool("quiet", false, "do not log requests")
	)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, `usage: typetown serve [flags]

Serve place lookups over HTTP.

    GET <path>?q=lond&limit=5&home=GB
    GET <path>?q=lond&lat=51.5&lon=-0.12
    GET /healthz

flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errParsed
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("serve: unexpected argument %q", fs.Arg(0))
	}

	ix, err := typetown.Open(*dir)
	if err != nil {
		return err
	}
	defer ix.Close()

	opts := []typetown.HandlerOption{typetown.WithLimit(*limit, *maxLimit)}
	if *home != "" {
		opts = append(opts, typetown.WithHome(*home))
	}
	if *homeHeader != "" {
		opts = append(opts, typetown.WithHomeHeader(*homeHeader))
	}
	if *cors != "" {
		opts = append(opts, typetown.WithCORS(splitOrigins(*cors)...))
	}

	mux := serveMux(ix, *path, opts, *quiet, env.Stderr)

	// Bind before announcing, so the address printed is one that is actually
	// listening — and so :0 reports the port the kernel chose.
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}
	stats := ix.Stats()
	fmt.Fprintf(env.Stdout, "typetown %s serving %d places on http://%s%s\n",
		Version(), stats.Records, ln.Addr(), *path)
	fmt.Fprintf(env.Stdout, "  try: curl '%s'\n",
		fmt.Sprintf("http://%s%s?q=lond&limit=3", ln.Addr(), *path))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Ctrl-C or a container stop should finish in-flight requests rather than
	// cutting them off.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		fmt.Fprintln(env.Stdout, "\nshutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}

// serveMux wires the lookup handler and a health endpoint onto a mux. It is
// separate from runServe so it can be exercised without binding a port.
func serveMux(ix *typetown.Index, path string, opts []typetown.HandlerOption, quiet bool, logTo io.Writer) http.Handler {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	mux := http.NewServeMux()
	mux.Handle(path, typetown.Handler(ix, opts...))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(ix.Stats())
	})
	if quiet {
		return mux
	}
	return logRequests(mux, logTo)
}

// logRequests writes one line per request: enough to see what a client asked
// for and what it got, and nothing more.
func logRequests(next http.Handler, out io.Writer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		target := r.URL.Path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		fmt.Fprintf(out, "%s %s %d %s\n", r.Method, target, rec.status,
			time.Since(start).Round(time.Microsecond))
	})
}

// statusRecorder remembers the status code so it can be logged after the fact.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func splitOrigins(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
