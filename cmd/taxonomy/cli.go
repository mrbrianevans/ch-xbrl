package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

var (
	errTTYStdout = errors.New("refusing to write CSV to the terminal; use -o FILE or -o -")
)

type config struct {
	output  string
	stdout  bool
	cache   string
	sources string
}

func parseConfig(args []string, stdoutIsTTY bool) (config, error) {
	fs := flag.NewFlagSet("ch-xbrl-taxonomy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	var output, cache, sources string
	fs.StringVar(&output, "o", "", "write CSV to `FILE` (default stdout)")
	fs.StringVar(&output, "output", "", "write CSV to `FILE` (default stdout)")
	fs.StringVar(&cache, "c", "", "directory for downloaded taxonomy zips")
	fs.StringVar(&cache, "cache", "", "directory for downloaded taxonomy zips")
	fs.StringVar(&sources, "s", "", "CSV of id,url,sha256 instead of the pinned list")
	fs.StringVar(&sources, "sources", "", "CSV of id,url,sha256 instead of the pinned list")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if extra := fs.Args(); len(extra) > 0 {
		return config{}, fmt.Errorf("unexpected extra arguments: %s", strings.Join(extra, " "))
	}

	cfg := config{cache: cache, sources: sources}
	switch output {
	case "":
		if stdoutIsTTY {
			return config{}, errTTYStdout
		}
		cfg.stdout = true
	case "-":
		cfg.stdout = true
	default:
		cfg.output = output
	}
	return cfg, nil
}

func stdoutIsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ch-xbrl-taxonomy [-o FILE] [-c DIR] [-s FILE]

Write a concept lookup CSV from the pinned UK taxonomy suites.
The key is namespace + local name. schemaRef is not part of it.
See docs/taxonomy-lookup.md. This tool is outside the ch-xbrl CLI contract.

  -o, --output FILE   write CSV to FILE (default: stdout)
  -c, --cache DIR     directory for downloaded taxonomy zips
  -s, --sources FILE  CSV of id,url,sha256 instead of the pinned list

Omit -o to write stdout when it is not a terminal (pipes, files).
On a TTY, pass -o FILE, or -o - to force stdout.

Exit codes: 0 success; 1 download, parse, or concept conflict; 2 usage.

Example:
  ch-xbrl-taxonomy -o concepts.csv
`)
}
