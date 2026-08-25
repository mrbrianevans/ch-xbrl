// Command ch-xbrl streams a remote or local archive of Companies House iXBRL
// accounts and writes a long-format fact CSV.
//
// Supported inputs (local path or http(s) URL):
//
//	.zip      — random access; remote uses HTTP range requests
//	.tar.zst  — sequential stream (optional plain .tar)
//
// Examples:
//
//	ch-xbrl -o facts.csv samples/sample.tar.zst
//	ch-xbrl -o facts.csv -workers 16 https://example.com/Accounts_Bulk_Data.tar.zst
//	ch-xbrl samples/sample.tar.zst > facts.csv
//	ch-xbrl -V
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
	cfg, err := parseConfig(os.Args[1:], stdoutIsTerminal())
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(os.Stderr)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "ch-xbrl: %v\n", err)
		if !errors.Is(err, errTTYStdout) {
			printUsage(os.Stderr)
		}
		os.Exit(2)
	}
	if cfg.showVersion {
		fmt.Println(versionLine())
		os.Exit(0)
	}

	var outW *os.File
	outName := "-"
	if cfg.stdout {
		outW = os.Stdout
	} else {
		outName = cfg.output
		outW, err = os.Create(cfg.output)
		if err != nil {
			log.Fatalf("create output: %v", err)
		}
		defer func() { _ = outW.Close() }()
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

	// Workers
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

	// Progress logger
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
	log.Printf("input: %s format=%s", cfg.input, archive.DetectFormat(cfg.input))
	n, streamErr := archive.Stream(ctx, cfg.input, members)
	wg.Wait()
	close(done)

	if err := csvW.Flush(); err != nil {
		log.Fatalf("flush: %v", err)
	}

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
		log.Fatalf("stream: %v", streamErr)
	}
	if filesOK.Load() == 0 {
		os.Exit(1)
	}
}
