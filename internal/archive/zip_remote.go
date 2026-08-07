package archive

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
)

// Remote ZIP batching defaults tuned for CloudFront→S3 bulk accounts packs
// (~100k members × ~100 KiB). Override in tests via package-level vars.
var (
	remoteRangeTarget  int64 = 16 << 20 // soft target span per Range GET
	remoteRangeMax     int64 = 32 << 20 // hard cap (single oversized member may exceed)
	remoteRangeWorkers       = 16
	remoteGapSplit     int64 = 1 << 20 // split batch if hole between members exceeds this
)

// memberBatch is a contiguous byte range covering one or more ZIP local files.
type memberBatch struct {
	start   int64 // inclusive absolute offset
	end     int64 // inclusive absolute offset
	entries []cdEntry
}

// streamZipRemote loads the central directory with a few range requests, packs
// members into large contiguous ranges, fetches those ranges in parallel, and
// inflates members into out. Designed for CloudFront-backed CH bulk ZIPs.
func streamZipRemote(ctx context.Context, source string, out chan<- Member) (int, error) {
	client := newRemoteHTTPClient()
	return streamZipRemoteWithClient(ctx, client, source, out)
}

func streamZipRemoteWithClient(ctx context.Context, client *http.Client, source string, out chan<- Member) (int, error) {
	size, err := remoteSize(ctx, client, source)
	if err != nil {
		return 0, fmt.Errorf("remote size: %w", err)
	}
	dir, err := loadRemoteZipDirectory(ctx, client, source, size)
	if err != nil {
		return 0, err
	}

	// Fence ends with CD start so the last member's span does not run into the directory.
	batches := packMemberBatches(dir.Entries, dir.CDOffset, remoteRangeTarget, remoteRangeMax, remoteGapSplit)
	if len(batches) == 0 {
		return 0, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan memberBatch, remoteRangeWorkers)
	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
		emitted  atomic.Int64
	)
	fail := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	workers := remoteRangeWorkers
	if workers < 1 {
		workers = 1
	}
	if workers > len(batches) {
		workers = len(batches)
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range jobs {
				if err := ctx.Err(); err != nil {
					return
				}
				n, err := processRemoteBatch(ctx, client, source, batch, out)
				if err != nil {
					fail(err)
					return
				}
				emitted.Add(int64(n))
			}
		}()
	}

	for _, b := range batches {
		if err := ctx.Err(); err != nil {
			break
		}
		select {
		case <-ctx.Done():
		case jobs <- b:
		}
	}
	close(jobs)
	wg.Wait()

	n := int(emitted.Load())
	if firstErr != nil {
		return n, firstErr
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	return n, nil
}

func processRemoteBatch(ctx context.Context, client *http.Client, url string, batch memberBatch, out chan<- Member) (int, error) {
	if batch.end < batch.start {
		return 0, fmt.Errorf("invalid batch range %d-%d", batch.start, batch.end)
	}
	data, err := rangeGET(ctx, client, url, batch.start, batch.end)
	if err != nil {
		return 0, fmt.Errorf("range %d-%d: %w", batch.start, batch.end, err)
	}
	n := 0
	for _, e := range batch.entries {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if e.UncompressedSize > maxMemberSize {
			return n, fmt.Errorf("member %s exceeds size limit", e.Name)
		}
		content, err := extractMemberFromRange(data, batch.start, e)
		if err != nil {
			return n, fmt.Errorf("extract %s: %w", e.Name, err)
		}
		if err := emit(ctx, out, e.Name, content); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// packMemberBatches groups wanted CD entries into contiguous HTTP range spans.
// all entries (wanted and not) are used as offset fences so local-header sizing
// does not depend on guessing extra-field lengths.
func packMemberBatches(all []cdEntry, cdOffset, target, maxSpan, gapSplit int64) []memberBatch {
	if len(all) == 0 {
		return nil
	}
	sorted := append([]cdEntry(nil), all...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].LocalHeaderOffset < sorted[j].LocalHeaderOffset
	})

	// endFence[i] = last inclusive byte for entry i (up to next header - 1, or CD - 1).
	// Using the next local-header offset as a fence avoids guessing local extra lengths.
	endFence := make([]int64, len(sorted))
	for i := range sorted {
		var next int64
		if i+1 < len(sorted) {
			next = sorted[i+1].LocalHeaderOffset
		} else {
			next = cdOffset
		}
		fence := next - 1
		if fence < sorted[i].LocalHeaderOffset {
			// Degenerate layout: fall back to a size estimate.
			fence = sorted[i].LocalHeaderOffset + localFileHeaderLen + int64(len(sorted[i].Name)) + 256 + int64(sorted[i].CompressedSize) - 1
		}
		if cdOffset > 0 && fence >= cdOffset {
			fence = cdOffset - 1
		}
		endFence[i] = fence
	}

	var (
		batches []memberBatch
		cur     *memberBatch
	)
	closeCur := func() {
		if cur != nil && len(cur.entries) > 0 {
			batches = append(batches, *cur)
		}
		cur = nil
	}

	for i, e := range sorted {
		name := filepath.ToSlash(e.Name)
		if !wantMember(name) {
			continue
		}

		start := e.LocalHeaderOffset
		end := endFence[i]

		if cur == nil {
			cur = &memberBatch{start: start, end: end, entries: []cdEntry{e}}
			if end-start+1 >= target {
				closeCur()
			}
			continue
		}

		gap := start - cur.end - 1
		if gap < 0 {
			gap = 0
		}
		newEnd := end
		span := newEnd - cur.start + 1
		if gap > gapSplit || (len(cur.entries) > 0 && span > maxSpan) {
			closeCur()
			cur = &memberBatch{start: start, end: end, entries: []cdEntry{e}}
			if end-start+1 >= target {
				closeCur()
			}
			continue
		}

		cur.end = newEnd
		cur.entries = append(cur.entries, e)
		if cur.end-cur.start+1 >= target {
			closeCur()
		}
	}
	closeCur()
	return batches
}

// extractMemberFromRange inflates one member whose local header lies in data,
// where data begins at absolute rangeStart in the archive.
func extractMemberFromRange(data []byte, rangeStart int64, e cdEntry) ([]byte, error) {
	rel := e.LocalHeaderOffset - rangeStart
	if rel < 0 || rel >= int64(len(data)) {
		return nil, fmt.Errorf("local header offset %d outside range starting %d (len %d)", e.LocalHeaderOffset, rangeStart, len(data))
	}
	off := int(rel)
	if off+localFileHeaderLen > len(data) {
		return nil, fmt.Errorf("truncated local header for %s", e.Name)
	}
	if binary.LittleEndian.Uint32(data[off:off+4]) != sigLocalFile {
		return nil, fmt.Errorf("bad local header signature for %s", e.Name)
	}
	nameLen := int(binary.LittleEndian.Uint16(data[off+26 : off+28]))
	extraLen := int(binary.LittleEndian.Uint16(data[off+28 : off+30]))
	bodyOff := off + localFileHeaderLen + nameLen + extraLen
	if bodyOff < off || bodyOff > len(data) {
		return nil, fmt.Errorf("invalid local header lengths for %s", e.Name)
	}
	compSize := int64(e.CompressedSize)
	if int64(bodyOff)+compSize > int64(len(data)) {
		return nil, fmt.Errorf("compressed data for %s exceeds range buffer (need %d have %d)", e.Name, int64(bodyOff)+compSize, len(data))
	}
	comp := data[bodyOff : bodyOff+int(compSize)]

	if e.UncompressedSize > maxMemberSize {
		return nil, fmt.Errorf("member %s exceeds size limit", e.Name)
	}

	var raw []byte
	switch e.Method {
	case methodStore:
		raw = make([]byte, len(comp))
		copy(raw, comp)
	case methodDeflate:
		fr := flate.NewReader(bytes.NewReader(comp))
		defer fr.Close()
		var err error
		raw, err = io.ReadAll(io.LimitReader(fr, int64(maxMemberSize)+1))
		if err != nil {
			return nil, err
		}
		if int64(len(raw)) > maxMemberSize {
			return nil, fmt.Errorf("member %s exceeds size limit", e.Name)
		}
	default:
		return nil, fmt.Errorf("unsupported compression method %d for %s", e.Method, e.Name)
	}

	// Prefer CD uncompressed size when set.
	if e.UncompressedSize > 0 && uint64(len(raw)) != e.UncompressedSize {
		return nil, fmt.Errorf("size mismatch for %s: got %d want %d", e.Name, len(raw), e.UncompressedSize)
	}
	if e.CRC32 != 0 {
		if crc32.ChecksumIEEE(raw) != e.CRC32 {
			return nil, fmt.Errorf("checksum mismatch for %s", e.Name)
		}
	}
	return raw, nil
}
