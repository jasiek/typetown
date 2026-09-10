package cli

import (
	"fmt"
	"runtime/debug"
)

// version is stamped in at build time by the Makefile:
//
//	go build -ldflags "-X github.com/jasiek/typetown/internal/cli.version=v1.2.3"
//
// When it is empty (plain `go build`, `go run`, `go install`) Version falls
// back to the module and VCS metadata the toolchain embeds in every binary.
var version string

// Version reports the version of this typetown build.
func Version() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	// Set for binaries installed from a tagged module, e.g. `go install`.
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	var revision string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision == "" {
		return "(devel)"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if dirty {
		revision += "-dirty"
	}
	return "(devel) " + revision
}

func runVersion(env Env, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("version: unexpected argument %q", args[0])
	}
	fmt.Fprintf(env.Stdout, "typetown %s\n", Version())
	return nil
}
