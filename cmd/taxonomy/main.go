// Command ch-xbrl-taxonomy writes a concept lookup CSV from pinned UK
// taxonomy packages. It is not the ch-xbrl extractor and it is outside
// the frozen CLI contract.
//
//	ch-xbrl-taxonomy -o concepts.csv
//	ch-xbrl-taxonomy -o concepts.csv -cache ~/.cache/ch-xbrl-taxonomy
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
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, stdoutIsTerminal()))
}

func run(args []string, stdout, stderr io.Writer, stdoutIsTTY bool) int {
	log.SetOutput(stderr)
	cfg, err := parseConfig(args, stdoutIsTTY)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stderr)
			return exitOK
		}
		fmt.Fprintf(stderr, "ch-xbrl-taxonomy: %v\n", err)
		if !errors.Is(err, errTTYStdout) {
			printUsage(stderr)
		}
		return exitUsage
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

	var out io.Writer = stdout
	var closer io.Closer
	if !cfg.stdout {
		f, err := os.Create(cfg.output)
		if err != nil {
			log.Printf("create output: %v", err)
			return exitFail
		}
		out = f
		closer = f
	}
	if err := taxonomy.WriteCSV(out, concepts); err != nil {
		if closer != nil {
			closer.Close()
		}
		log.Printf("write: %v", err)
		return exitFail
	}
	if closer != nil {
		if err := closer.Close(); err != nil {
			log.Printf("close: %v", err)
			return exitFail
		}
		log.Printf("wrote %s (%d concepts)", cfg.output, len(concepts))
	} else {
		log.Printf("wrote %d concepts", len(concepts))
	}
	return exitOK
}
