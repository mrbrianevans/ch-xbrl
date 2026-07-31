// Package csvout provides a concurrency-safe CSV writer for fact rows.
package csvout

import (
	"encoding/csv"
	"io"
	"sync"

	"github.com/mrbrianevans/ch-xbrl/internal/fact"
)

// Writer writes fact.Fact rows to a CSV stream. Write is safe for concurrent use.
type Writer struct {
	mu     sync.Mutex
	w      *csv.Writer
	header bool
}

// New creates a Writer that writes to w. The header row is written on first Write.
func New(w io.Writer) *Writer {
	return &Writer{w: csv.NewWriter(w)}
}

// Write appends one fact row.
func (cw *Writer) Write(f fact.Fact) error {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if !cw.header {
		if err := cw.w.Write(fact.CSVHeader); err != nil {
			return err
		}
		cw.header = true
	}
	return cw.w.Write(f.Record())
}

// WriteAll writes multiple facts.
func (cw *Writer) WriteAll(facts []fact.Fact) error {
	for _, f := range facts {
		if err := cw.Write(f); err != nil {
			return err
		}
	}
	return nil
}

// Flush flushes the underlying CSV buffer.
func (cw *Writer) Flush() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.w.Flush()
	return cw.w.Error()
}
