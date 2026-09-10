// Package cli implements typetown's command-line interface: flag parsing,
// subcommand dispatch and usage output.
//
// Everything lives here rather than in package main so that the CLI can be
// exercised from tests without spawning a subprocess.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
)

// errParsed reports that a subcommand's flag set has already printed the
// problem, so Run should exit with a usage code without printing anything more.
var errParsed = errors.New("flag parse error")

// Exit codes, following the usual Unix convention.
const (
	exitOK    = 0 // the command did what was asked
	exitError = 1 // the command ran and failed
	exitUsage = 2 // the command line itself was wrong
)

// Env is everything a command needs from the outside world. Commands write to
// these writers instead of os.Stdout/os.Stderr so tests can capture output.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// command is a single subcommand. run receives the arguments that follow the
// subcommand name, with the name itself already stripped.
type command struct {
	name    string
	summary string
	run     func(env Env, args []string) error
}

// commands is the subcommand registry. Add new subcommands here.
var commands = []command{
	{
		name:    "build",
		summary: "build an index from the GeoNames inputs",
		run:     runBuild,
	},
	{
		name:    "query",
		summary: "look up places by name prefix (-i for interactive)",
		run:     runQuery,
	},
	{
		name:    "version",
		summary: "print the typetown version",
		run:     runVersion,
	},
}

// Run executes typetown with args (excluding the program name) and returns the
// process exit code.
func Run(env Env, args []string) int {
	fs := flag.NewFlagSet("typetown", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() { usage(env.Stderr) }

	if err := fs.Parse(args); err != nil {
		// -h and -help are a deliberate request for help, not a mistake.
		if errors.Is(err, flag.ErrHelp) {
			usage(env.Stdout)
			return exitOK
		}
		// flag has already reported the problem on env.Stderr.
		return exitUsage
	}

	rest := fs.Args()
	if len(rest) == 0 {
		usage(env.Stderr)
		return exitUsage
	}

	name, rest := rest[0], rest[1:]
	if name == "help" {
		usage(env.Stdout)
		return exitOK
	}

	for _, c := range commands {
		if c.name != name {
			continue
		}
		switch err := c.run(env, rest); {
		case err == nil:
			return exitOK
		case errors.Is(err, errParsed):
			// The subcommand's flag set already reported the problem.
			return exitUsage
		default:
			fmt.Fprintf(env.Stderr, "typetown: %v\n", err)
			return exitError
		}
	}

	fmt.Fprintf(env.Stderr, "typetown: unknown command %q\n\n", name)
	usage(env.Stderr)
	return exitUsage
}

// usage writes the top-level help text to w.
func usage(w io.Writer) {
	fmt.Fprint(w, "usage:\n  typetown <command> [arguments]\n\ncommands:\n")

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(tw, "  %s\t%s\n", c.name, c.summary)
	}
	fmt.Fprintf(tw, "  %s\t%s\n", "help", "show this help")
	tw.Flush()
}
