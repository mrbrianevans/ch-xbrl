// Command ch-xbrl streams Companies House iXBRL accounts and writes a
// long-format fact CSV.
//
// Supported inputs (one positional path, URL, or "-"):
//
//	.zip / .tar.zst / .tar  — local or http(s); remote zip uses HTTP ranges
//	.xhtml .html .htm .xbrl .xml — single instance, local or http(s)
//	http(s) URL with no known extension — GET, follow redirects, sniff body
//	directory               — non-recursive; top-level instance files only
//	-                       — stdin; XML/XHTML, tar, or tar.zst (not zip)
//
// Examples:
//
//	ch-xbrl -o facts.csv samples/sample.tar.zst
//	ch-xbrl -o facts.csv samples/03024914_aa_2023-03-13.xhtml
//	ch-xbrl -o facts.csv samples/
//	ch-xbrl -o facts.csv -workers 16 https://example.com/Accounts_Bulk_Data.tar.zst
//	ch-xbrl samples/sample.tar.zst > facts.csv
//	cat accounts.xhtml | ch-xbrl -o facts.csv -
//	ch-xbrl -V
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
	"github.com/mrbrianevans/ch-xbrl/internal/csvout"
	"github.com/mrbrianevans/ch-xbrl/internal/ixbrl"
)

// memberQueueDepth is the buffered channel size between archive streaming and
// parse workers. Large enough to keep tar/zip producers from stalling on a
// busy worker pool; small enough that ~100 KiB CH accounts stay cheap in RAM.
const memberQueueDepth = 64

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, stdoutIsTerminal()))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, stdoutIsTTY bool) int {
	log.SetOutput(stderr)
	cfg, err := parseConfig(args, stdoutIsTTY)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stderr)
			return exitOK
		}
		fmt.Fprintf(stderr, "ch-xbrl: %v\n", err)
		if !errors.Is(err, errTTYStdout) {
			printUsage(stderr)
		}
		return exitUsage
	}
	if cfg.showVersion {
		fmt.Fprintln(stdout, versionLine())
		return exitOK
	}

	var outW io.Writer = stdout
	outName := "-"
	var outFile *os.File
	if !cfg.stdout {
		outName = cfg.output
		outFile, err = os.Create(cfg.output)
		if err != nil {
			log.Printf("create output: %v", err)
			return runExitCode(0, 0, err)
		}
		defer func() { _ = outFile.Close() }()
		outW = outFile
	}
	csvW := csvout.New(outW)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	members := make(chan archive.Member, memberQueueDepth)
	var (
		filesOK   atomic.Int64
		filesErr  atomic.Int64
		factCount atomic.Int64
		errMu     sync.Mutex
		firstErrs []string
	)

	var wg sync.WaitGroup
	for i := 0; i < cfg.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range members {
				facts, err := ixbrl.ParseBytes(m.Content, m.Name)
				if err != nil {
					filesErr.Add(1)
					errMu.Lock()
					if len(firstErrs) < 20 {
						firstErrs = append(firstErrs, fmt.Sprintf("%s: %v", m.Name, err))
					}
					errMu.Unlock()
					continue
				}
				if err := csvW.WriteAll(facts); err != nil {
					filesErr.Add(1)
					log.Printf("csv write %s: %v", m.Name, err)
					continue
				}
				filesOK.Add(1)
				factCount.Add(int64(len(facts)))
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				log.Printf("progress: files_ok=%d files_err=%d facts=%d",
					filesOK.Load(), filesErr.Load(), factCount.Load())
			}
		}
	}()

	start := time.Now()
	log.Printf("input: %s format=%s", cfg.input, archive.Describe(cfg.input))
	n, streamErr := archive.StreamFrom(ctx, cfg.input, stdin, members)
	wg.Wait()
	close(done)

	flushErr := csvW.Flush()

	elapsed := time.Since(start).Round(time.Millisecond)
	log.Printf("done: members=%d files_ok=%d files_err=%d facts=%d elapsed=%s out=%s",
		n, filesOK.Load(), filesErr.Load(), factCount.Load(), elapsed, outName)

	if len(firstErrs) > 0 {
		log.Printf("sample errors (%d total err files):", filesErr.Load())
		for _, e := range firstErrs {
			log.Printf("  %s", e)
		}
	}
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) {
		log.Printf("stream: %v", streamErr)
	}
	if flushErr != nil {
		log.Printf("flush: %v", flushErr)
		if streamErr == nil {
			streamErr = flushErr
		}
	}
	return runExitCode(filesOK.Load(), filesErr.Load(), streamErr)
}
