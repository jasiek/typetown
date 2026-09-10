package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"typetown/internal/index"
)

// Control bytes we act on. Everything else printable is appended to the query.
const (
	keyCtrlC     = 0x03
	keyCtrlD     = 0x04
	keyCtrlU     = 0x15
	keyCtrlW     = 0x17
	keyEnter     = 0x0d
	keyLineFeed  = 0x0a
	keyEscape    = 0x1b
	keyBackspace = 0x7f
	keyDelete    = 0x08
)

// live is the incremental search UI: results are redrawn on every keystroke.
//
// It draws to a separate writer from the one the chosen result goes to, so the
// redraw churn stays on stderr and `$(typetown query -i)` captures only the
// answer.
type live struct {
	ix   *index.Index
	opts index.SearchOptions
	ui   io.Writer // the terminal, redrawn constantly
	out  io.Writer // where the final selection is written
	json bool

	fd    int
	query []rune
	res   []index.Result
	sel   int
	took  time.Duration
	drawn int // lines the last frame occupied, so the next one can erase it
}

// liveSearch runs the incremental UI. The terminal is put into raw mode so that
// keystrokes arrive without waiting for a newline, and restored on every exit
// path — including panics — because a terminal left in raw mode is unusable.
func liveSearch(env Env, ix *index.Index, opts index.SearchOptions, asJSON bool, fd int) (err error) {
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("enter raw mode: %w", err)
	}
	defer func() {
		if rerr := term.Restore(fd, state); rerr != nil && err == nil {
			err = fmt.Errorf("restore terminal: %w", rerr)
		}
	}()

	l := &live{ix: ix, opts: opts, ui: env.Stderr, out: env.Stdout, json: asJSON, fd: fd}
	return l.run(bufio.NewReader(env.Stdin))
}

func (l *live) run(in *bufio.Reader) error {
	l.search()
	l.draw()

	for {
		r, _, err := in.ReadRune()
		if err == io.EOF {
			return l.finish(false)
		}
		if err != nil {
			return err
		}

		switch r {
		case keyCtrlC:
			return l.finish(false)
		case keyCtrlD:
			if len(l.query) == 0 {
				return l.finish(false)
			}
		case keyEnter, keyLineFeed:
			return l.finish(true)
		case keyBackspace, keyDelete:
			if n := len(l.query); n > 0 {
				l.query = l.query[:n-1]
				l.search()
			}
		case keyCtrlU:
			l.query = l.query[:0]
			l.search()
		case keyCtrlW:
			l.deleteWord()
			l.search()
		case keyEscape:
			if l.handleEscape(in) {
				continue // a cursor key: selection moved, no new search needed
			}
			return l.finish(false)
		default:
			if r >= 0x20 && r != 0x7f {
				l.query = append(l.query, r)
				l.search()
			} else {
				continue // a control byte we do not act on
			}
		}
		l.draw()
	}
}

// handleEscape consumes a CSI sequence. It reports whether the sequence was
// understood; a bare ESC (nothing follows) means the user wants out.
func (l *live) handleEscape(in *bufio.Reader) bool {
	if in.Buffered() == 0 {
		return false // a lone Escape
	}
	b, err := in.ReadByte()
	if err != nil || b != '[' {
		return false
	}
	c, err := in.ReadByte()
	if err != nil {
		return false
	}
	switch c {
	case 'A': // up
		if l.sel > 0 {
			l.sel--
		}
	case 'B': // down
		if l.sel < len(l.res)-1 {
			l.sel++
		}
	default:
		return true // some other sequence; swallow it rather than typing it
	}
	l.draw()
	return true
}

func (l *live) deleteWord() {
	i := len(l.query)
	for i > 0 && l.query[i-1] == ' ' {
		i--
	}
	for i > 0 && l.query[i-1] != ' ' {
		i--
	}
	l.query = l.query[:i]
}

func (l *live) search() {
	l.sel = 0
	q := string(l.query)
	if strings.TrimSpace(q) == "" {
		l.res, l.took = nil, 0
		return
	}
	start := time.Now()
	res, err := l.ix.Search(q, l.opts)
	l.took = time.Since(start)
	if err != nil {
		l.res = nil
		return
	}
	l.res = res
}

const prompt = "> "

// draw repaints the frame in place. The cursor is always left on the prompt
// line, just after the query, so the next frame can erase from there downward
// without needing to know where it ended up.
func (l *live) draw() {
	width, _, err := term.GetSize(l.fd)
	if err != nil || width < 20 {
		width = 80
	}

	var b strings.Builder
	if l.drawn > 1 {
		fmt.Fprintf(&b, "\x1b[%dA", l.drawn-1) // back to the prompt line
	}
	b.WriteString("\r\x1b[0J") // and erase everything from here down

	b.WriteString(prompt)
	b.WriteString(string(l.query))
	lines := 1

	for i, r := range l.res {
		b.WriteString("\r\n")
		line := truncate(fmt.Sprintf("  %-46s  %9.4f, %9.4f  %s", place(r), r.Lat, r.Lon, detail(r)), width-1)
		if i == l.sel {
			b.WriteString("\x1b[7m" + line + "\x1b[0m")
		} else {
			b.WriteString(line)
		}
		lines++
	}

	b.WriteString("\r\n")
	b.WriteString(truncate(l.status(), width-1))
	lines++

	// Return to the prompt line and sit just after the query text.
	fmt.Fprintf(&b, "\x1b[%dA\r\x1b[%dC", lines-1, utf8.RuneCountInString(prompt)+len(l.query))

	l.drawn = lines
	io.WriteString(l.ui, b.String())
}

func (l *live) status() string {
	switch {
	case len(l.query) == 0:
		m := l.ix.Manifest()
		return fmt.Sprintf("\x1b[2m%d places · ↑↓ select · enter accept · esc quit\x1b[0m", m.Records)
	case len(l.res) == 0:
		return "\x1b[2mno matches\x1b[0m"
	default:
		return fmt.Sprintf("\x1b[2m%d results in %s\x1b[0m", len(l.res), l.took.Round(time.Microsecond))
	}
}

// finish erases the UI and, when the user accepted a result, writes it to the
// output stream.
func (l *live) finish(accept bool) error {
	if l.drawn > 1 {
		fmt.Fprintf(l.ui, "\x1b[%dA", l.drawn-1)
	}
	io.WriteString(l.ui, "\r\x1b[0J")
	l.drawn = 0

	if !accept || l.sel >= len(l.res) {
		return nil
	}
	r := l.res[l.sel]
	if l.json {
		enc := json.NewEncoder(l.out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	_, err := fmt.Fprintf(l.out, "%s\t%.5f\t%.5f\n", place(r), r.Lat, r.Lon)
	return err
}

func truncate(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(stripANSI(s)) <= max {
		return s
	}
	// Only plain lines are ever long enough to need this, so a rune-count cut is
	// safe: escape-bearing lines are the short status strings.
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
