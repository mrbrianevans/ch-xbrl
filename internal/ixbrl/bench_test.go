package ixbrl

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
	"github.com/mrbrianevans/ch-xbrl/internal/csvout"
)

// sampleFiling is one document the CLI would pass to ParseDocument or
// ParseDocumentOnlyTarget. Zip packages contribute the selected report
// bytes, not the zip container. Setup reads those bytes once; the benchmark
// loop only parses.
type sampleFiling struct {
	benchName string
	source    string
	data      []byte
	onlyUKFRS bool
	facts     int
}

var (
	sampleOnce sync.Once
	sampleDocs []sampleFiling
	sampleErr  error
)

func loadSampleFilings(tb testing.TB) []sampleFiling {
	tb.Helper()
	sampleOnce.Do(func() {
		sampleDocs, sampleErr = readSampleFilings()
	})
	if sampleErr != nil {
		tb.Fatal(sampleErr)
	}
	return sampleDocs
}

func readSampleFilings() ([]sampleFiling, error) {
	dir := filepath.Join("..", "..", "samples")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read samples: %w", err)
	}
	var names []string
	for _, ent := range entries {
		if ent.IsDir() || !isBenchSample(ent.Name()) {
			continue
		}
		names = append(names, ent.Name())
	}
	sort.Strings(names)
	var out []sampleFiling
	seen := map[string]int{}
	for _, name := range names {
		filings, err := filingsAt(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		for _, f := range filings {
			n := seen[f.benchName]
			seen[f.benchName] = n + 1
			if n > 0 {
				f.benchName = fmt.Sprintf("%s#%d", f.benchName, n+1)
			}
			if err := annotateFiling(&f); err != nil {
				return nil, err
			}
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no sample filings in %s", dir)
	}
	return out, nil
}

func isBenchSample(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xhtml", ".html", ".htm", ".xbrl", ".xml", ".zip":
		return true
	default:
		return false
	}
}

func filingsAt(path string) ([]sampleFiling, error) {
	name := filepath.Base(path)
	if !strings.EqualFold(filepath.Ext(name), ".zip") {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []sampleFiling{{benchName: name, source: name, data: data}}, nil
	}
	r, err := zip.OpenReader(path)
	if err != nil {
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		// iXBRL published under a .zip name. A bulk member with these bytes
		// is parsed by ParseDocument. A positional path ending in .zip is
		// still opened as an archive and would fail here.
		if !looksLikeInstance(data) {
			return nil, fmt.Errorf("%s: not a zip archive or an instance", name)
		}
		return []sampleFiling{{benchName: name, source: name, data: data}}, nil
	}
	_ = r.Close()

	members, err := streamMembers(path)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("%s: zip yielded no filings", name)
	}
	out := make([]sampleFiling, 0, len(members))
	for i, m := range members {
		if m.Err != nil {
			return nil, fmt.Errorf("%s: %w", m.Name, m.Err)
		}
		bench := name
		if len(members) > 1 {
			bench = fmt.Sprintf("%s#%d", name, i+1)
		}
		out = append(out, sampleFiling{
			benchName: bench,
			source:    m.Name,
			data:      m.Content,
			onlyUKFRS: m.OnlyUKFRS,
		})
	}
	return out, nil
}

func looksLikeInstance(data []byte) bool {
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		data = data[3:]
	}
	data = bytes.TrimLeft(data, " \t\r\n")
	return bytes.HasPrefix(data, []byte("<"))
}

func streamMembers(path string) ([]archive.Member, error) {
	prev := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(prev)

	ch := make(chan archive.Member, 4)
	errc := make(chan error, 1)
	go func() {
		_, err := archive.StreamFrom(context.Background(), path, nil, ch)
		errc <- err
	}()
	var members []archive.Member
	for m := range ch {
		members = append(members, m)
	}
	if err := <-errc; err != nil {
		return nil, fmt.Errorf("stream %s: %w", path, err)
	}
	return members, nil
}

func annotateFiling(f *sampleFiling) error {
	doc, err := parseFiling(*f)
	if err != nil {
		return fmt.Errorf("parse %s: %w", f.benchName, err)
	}
	if len(doc.Facts) == 0 {
		return fmt.Errorf("parse %s: no facts", f.benchName)
	}
	f.facts = len(doc.Facts)
	return nil
}

// parseFiling is the CLI's per-document entry point. Report packages pass
// target UKFRS; every other filing uses ParseDocument.
func parseFiling(f sampleFiling) (Document, error) {
	if f.onlyUKFRS {
		return ParseDocumentOnlyTarget(f.data, f.source, "UKFRS")
	}
	return ParseDocument(f.data, f.source)
}

func benchFilings(b *testing.B, filings []sampleFiling, writeCSV bool) {
	b.Helper()
	var nbytes int64
	for _, f := range filings {
		nbytes += int64(len(f.data))
	}
	b.SetBytes(nbytes)

	var w *csvout.Writer
	if writeCSV {
		w = csvout.New(io.Discard)
	}
	b.ResetTimer()
	var sink Document
	for i := 0; i < b.N; i++ {
		for fi := range filings {
			doc, err := parseFiling(filings[fi])
			if err != nil {
				b.Fatalf("parse %s: %v", filings[fi].benchName, err)
			}
			if w != nil {
				if err := w.WriteAll(doc.Facts); err != nil {
					b.Fatalf("csv %s: %v", filings[fi].benchName, err)
				}
			}
			sink = doc
		}
	}
	b.StopTimer()
	if w != nil {
		if err := w.Flush(); err != nil {
			b.Fatal(err)
		}
	}
	if len(sink.Facts) == 0 {
		b.Fatal("no facts")
	}
}

// BenchmarkParseDocument parses every filing under samples/.
// Sub-benchmarks: all (byte-weighted corpus), report-package (target UKFRS),
// other (everything else), and one benchmark per filing.
func BenchmarkParseDocument(b *testing.B) {
	filings := loadSampleFilings(b)
	b.Run("all", func(b *testing.B) { benchFilings(b, filings, false) })

	var report, other []sampleFiling
	for _, f := range filings {
		if f.onlyUKFRS {
			report = append(report, f)
		} else {
			other = append(other, f)
		}
	}
	if len(report) > 0 {
		b.Run("report-package", func(b *testing.B) { benchFilings(b, report, false) })
	}
	if len(other) > 0 {
		b.Run("other", func(b *testing.B) { benchFilings(b, other, false) })
	}
	for _, f := range filings {
		b.Run(f.benchName, func(b *testing.B) { benchFilings(b, []sampleFiling{f}, false) })
	}
}

// BenchmarkParseAndWriteCSV parses the corpus and writes facts the way the
// CLI does, to io.Discard, on one goroutine. The writer lock is uncontended.
func BenchmarkParseAndWriteCSV(b *testing.B) {
	filings := loadSampleFilings(b)
	b.Run("all", func(b *testing.B) { benchFilings(b, filings, true) })
}

// TestSampleFilings parses the benchmark corpus once, so a filing that stops
// parsing fails go test and not only the benchmark. It logs what each
// filing contributes.
func TestSampleFilings(t *testing.T) {
	filings := loadSampleFilings(t)
	var nbytes, nfacts int
	for _, f := range filings {
		nbytes += len(f.data)
		nfacts += f.facts
		kind := "other"
		if f.onlyUKFRS {
			kind = "report-package"
		}
		t.Logf("%s %s bytes=%d facts=%d", kind, f.benchName, len(f.data), f.facts)
	}
	t.Logf("filings=%d bytes=%d facts=%d", len(filings), nbytes, nfacts)
}
