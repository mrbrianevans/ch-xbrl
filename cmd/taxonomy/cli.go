package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

var (
	errMissingOutput = errors.New("missing -o DIR")
	errStdoutOutput  = errors.New("output is a directory; pass -o DIR")
)

type config struct {
	output  string
	cache   string
	sources string
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("ch-xbrl-taxonomy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	var output, cache, sources string
	fs.StringVar(&output, "o", "", "directory to create or update")
	fs.StringVar(&output, "output", "", "directory to create or update")
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
	if strings.TrimSpace(output) == "" {
		return config{}, errMissingOutput
	}
	if output == "-" {
		return config{}, errStdoutOutput
	}
	return config{output: filepath.Clean(output), cache: cache, sources: sources}, nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ch-xbrl-taxonomy -o DIR [-c DIR] [-s FILE]

Write concept lookup files into DIR from the pinned UK taxonomy suites.
The key is namespace + local name. schemaRef is not part of it.
See docs/taxonomy-lookup.md. This tool is outside the ch-xbrl CLI contract.

  -o, --output DIR    directory to create or update (required)
  -c, --cache DIR     directory for downloaded taxonomy zips
  -s, --sources FILE  CSV of id,url,sha256 instead of the pinned list

DIR is created if it is missing, including parent directories.
An existing directory is updated whether or not it already has files.
A symlink at DIR is followed when it points at a directory.
Files this tool owns are replaced. Every other file in DIR is left
as it is. If the path exists and is not a directory, the run fails.

Files written (names are fixed):

  concepts.csv    one row per namespace + local name

Exit codes: 0 success; 1 download, parse, concept conflict, or output
path; 2 usage.

Example:
  ch-xbrl-taxonomy -o my-dir/
`)
}
