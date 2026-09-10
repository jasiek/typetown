// Command typetown is the typetown command-line tool.
package main

import (
	"os"

	"typetown/internal/cli"
)

func main() {
	env := cli.Env{Stdout: os.Stdout, Stderr: os.Stderr}
	os.Exit(cli.Run(env, os.Args[1:]))
}
