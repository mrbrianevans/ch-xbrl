package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"
)

var (
	errMissingInput = errors.New("missing input path or URL")
	errTTYStdout    = errors.New("refusing to write CSV to the terminal; use -o FILE or -o -")
)

type config struct {
	input       string
	output      string // file path; empty when writing stdout
	stdout      bool
	workers     int
	showVersion bool
}

func parseConfig(args []string, stdoutIsTTY bool) (config, error) {
	fs := flag.NewFlagSet("ch-xbrl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	var output string
	var showVersion bool
	fs.StringVar(&output, "o", "", "write CSV to `FILE` (default stdout)")
	fs.StringVar(&output, "output", "", "write CSV to `FILE` (default stdout)")
	fs.BoolVar(&showVersion, "V", false, "print version and exit")
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	workers := fs.Int("workers", runtime.NumCPU(), "concurrent XBRL parse workers")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if showVersion {
		return config{showVersion: true}, nil
	}

	pos := fs.Args()
	switch {
	case len(pos) == 0 || strings.TrimSpace(pos[0]) == "":
		return config{}, errMissingInput
	case len(pos) > 1:
		return config{}, fmt.Errorf("unexpected extra arguments: %s", strings.Join(pos[1:], " "))
	}

	w := *workers
	if w < 1 {
		w = 1
	}
	cfg := config{input: pos[0], workers: w}

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
	fmt.Fprint(w, `usage: ch-xbrl [-o FILE] [-workers N] <path|url>
       ch-xbrl -V

Stream a local or remote Companies House iXBRL archive
(.zip, .tar.zst, or .tar) to a long-format fact CSV.

  -o, --output FILE   write CSV to FILE (default: stdout)
  -workers N          concurrent parse workers (default: number of CPUs)
  -V, --version       print version and exit

Omit -o to write stdout when it is not a terminal (pipes, files).
On a TTY, pass -o FILE, or -o - to force stdout.

Examples:
  ch-xbrl -o facts.csv samples/sample.tar.zst
  ch-xbrl -o facts.csv https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip
  ch-xbrl samples/sample.tar.zst > facts.csv
`)
}
