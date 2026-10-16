// Command ch-xbrl-taxonomy writes concept lookup files into a directory
// from pinned UK taxonomy packages. It is not the ch-xbrl extractor and
// it is outside the frozen CLI contract.
//
//	ch-xbrl-taxonomy -o my-dir/
//	ch-xbrl-taxonomy -o my-dir/ -c ~/.cache/ch-xbrl-taxonomy
//
// The file names inside the directory are fixed. Today that is
// my-dir/concepts.csv.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/mrbrianevans/ch-xbrl/internal/taxonomy"
)

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	log.SetOutput(stderr)
	cfg, err := parseConfig(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stderr)
			return exitOK
		}
		fmt.Fprintf(stderr, "ch-xbrl-taxonomy: %v\n", err)
		printUsage(stderr)
		return exitUsage
	}

	if err := ensureOutputDir(cfg.output); err != nil {
		log.Printf("%v", err)
		return exitFail
	}

	sources := taxonomy.Pinned()
	if cfg.sources != "" {
		f, err := os.Open(cfg.sources)
		if err != nil {
			log.Printf("sources: %v", err)
			return exitFail
		}
		sources, err = taxonomy.ParseSources(f)
		f.Close()
		if err != nil {
			log.Printf("sources: %v", err)
			return exitFail
		}
	}

	cache := cfg.cache
	if cache == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			log.Printf("cache: %v", err)
			return exitFail
		}
		cache = filepath.Join(base, "ch-xbrl-taxonomy")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	concepts, err := taxonomy.Build(ctx, sources, cache, nil, log.Printf)
	if err != nil {
		log.Printf("%v", err)
		return exitFail
	}

	dest := filepath.Join(cfg.output, conceptsCSV)
	if err := writeAtomic(cfg.output, conceptsCSV, func(w io.Writer) error {
		return taxonomy.WriteCSV(w, concepts)
	}); err != nil {
		log.Printf("write: %v", err)
		return exitFail
	}
	log.Printf("wrote %s (%d concepts)", dest, len(concepts))
	return exitOK
}
