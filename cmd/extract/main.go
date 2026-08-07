// Command extract streams a remote or local archive of Companies House iXBRL
// accounts and writes a long-format fact CSV.
//
// Supported inputs (local path or http(s) URL):
//
//	.zip      — random access; remote uses HTTP range requests
//	.tar.zst  — sequential stream (optional plain .tar)
//
// Examples:
//
//	extract -in samples/sample.tar.zst -out data/facts.csv
//	extract -in https://example.com/Accounts_Bulk_Data.tar.zst -out facts.csv -workers 16
//	extract -in https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip -out facts.csv
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
	"github.com/mrbrianevans/ch-xbrl/internal/csvout"
	"github.com/mrbrianevans/ch-xbrl/internal/ixbrl"
)

func main() {
	in := flag.String("in", "", "local path or https URL of a .zip, .tar.zst, or .tar archive")
	out := flag.String("out", "facts.csv", "output CSV path (use - for stdout)")
	workers := flag.Int("workers", runtime.NumCPU(), "concurrent XBRL parse workers")
	queue := flag.Int("queue", 64, "member queue depth")
	flag.Parse()

	if *in == "" {
		fmt.Fprintln(os.Stderr, "usage: extract -in <path|url> [-out facts.csv] [-workers N]")
		fmt.Fprintln(os.Stderr, "  -in accepts local or remote .zip / .tar.zst / .tar")
		flag.PrintDefaults()
		os.Exit(2)
	}
	if *workers < 1 {
		*workers = 1
	}

	var outW *os.File
	var err error
	if *out == "-" {
		outW = os.Stdout
	} else {
		outW, err = os.Create(*out)
		if err != nil {
			log.Fatalf("create output: %v", err)
		}
		defer outW.Close()
	}
	csvW := csvout.New(outW)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	members := make(chan archive.Member, *queue)
	var (
		filesOK   atomic.Int64
		filesErr  atomic.Int64
		factCount atomic.Int64
		errMu     sync.Mutex
		firstErrs []string
	)

	// Workers
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
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
	log.Printf("input: %s format=%s", *in, archive.DetectFormat(*in))
	n, streamErr := archive.Stream(ctx, *in, members)
	wg.Wait()
	close(done)

	if err := csvW.Flush(); err != nil {
		log.Fatalf("flush: %v", err)
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	log.Printf("done: members=%d files_ok=%d files_err=%d facts=%d elapsed=%s out=%s",
		n, filesOK.Load(), filesErr.Load(), factCount.Load(), elapsed, *out)

	if len(firstErrs) > 0 {
		log.Printf("sample errors (%d total err files):", filesErr.Load())
		for _, e := range firstErrs {
			log.Printf("  %s", e)
		}
	}
	if streamErr != nil && streamErr != context.Canceled {
		log.Fatalf("stream: %v", streamErr)
	}
	if filesOK.Load() == 0 {
		os.Exit(1)
	}
}
