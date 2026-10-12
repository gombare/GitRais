// genrepo writes to stdout a git fast-import stream with N commits spread over
// K parallel branches with periodic merges:
//
//	git init -q big && go run ./cmd/genrepo -n 1000000 | git -C big fast-import --quiet
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"gitrais/internal/genrepo"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("genrepo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	n := fs.Int("n", 1_000_000, "number of commits")
	k := fs.Int("branches", 12, "number of parallel branches")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "genrepo: unexpected arguments: %v\n", fs.Args())
		return 2
	}
	if err := genrepo.Validate(*n, *k); err != nil {
		fmt.Fprintln(stderr, "genrepo:", err)
		return 2
	}
	if err := genrepo.Write(stdout, *n, *k); err != nil {
		fmt.Fprintln(stderr, "genrepo:", err)
		return 1
	}
	return 0
}
