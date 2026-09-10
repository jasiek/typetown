// Command typetown is the typetown command-line tool.
package main

import (
	"os"

	"github.com/jasiek/typetown/internal/cli"
)

func main() {
	env := cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	os.Exit(cli.Run(env, os.Args[1:]))
}
