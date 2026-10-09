package archive

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
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
// sourceName, when set, is the filing name written on every emitted member
// (a package input). onlyUKFRS selects the report-package fact filter.
type memberBatch struct {
	start      int64 // inclusive absolute offset
	end        int64 // inclusive absolute offset
	entries    []cdEntry
	sourceName string
	onlyUKFRS  bool
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

	names := make([]string, len(dir.Entries))
	for i, e := range dir.Entries {
		names[i] = filepath.ToSlash(e.Name)
	}
	if sel := selectPackage(names); sel.Kind != packageBulk {
		return streamRemotePackage(ctx, client, source, dir, sel, out)
	}

	nBig, err := emitOversizedEntries(ctx, dir.Entries, out)
	if err != nil {
		return nBig, err
	}

	// Fence ends with CD start so the last member's span does not run into the directory.
	batches := packMemberBatches(dir.Entries, dir.CDOffset, remoteRangeTarget, remoteRangeMax, remoteGapSplit)
	if len(batches) == 0 {
		return nBig, nil
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

	n := nBig + int(emitted.Load())
	if firstErr != nil {
		return n, firstErr
	}
	if err := ctx.Err(); err != nil {
		return n, err
	}
	return n, nil
}

// streamRemotePackage fetches only the selected reports and emits them as one
// filing. source_file is the URL basename. An empty selection, or a selected
// report over the size limit, is a member error.
func streamRemotePackage(ctx context.Context, client *http.Client, source string, dir *zipDirectory, sel packageSelection, out chan<- Member) (int, error) {
	sourceName := instanceName(source)
	if len(sel.Files) == 0 {
		return emitNoReports(ctx, sourceName, out)
	}
	allow := map[string]bool{}
	for _, p := range sel.Files {
		allow[p] = true
	}
	for _, e := range dir.Entries {
		if !allow[normZipName(e.Name)] {
			continue
		}
		if e.UncompressedSize > uint64(maxMemberSize) {
			return emitTooBig(ctx, sourceName, out)
		}
	}
	batches := packMemberBatchesIf(dir.Entries, dir.CDOffset, remoteRangeTarget, remoteRangeMax, remoteGapSplit, allow)
	for i := range batches {
		batches[i].sourceName = sourceName
		batches[i].onlyUKFRS = sel.UKFRS
	}
	if len(batches) == 0 {
		return emitNoReports(ctx, sourceName, out)
	}
	// One package is small. Read it on this goroutine.
	n := 0
	for _, batch := range batches {
		added, err := processRemoteBatch(ctx, client, source, batch, out)
		if err != nil {
			return n, err
		}
		n += added
	}
	if n == 0 {
		return emitNoReports(ctx, sourceName, out)
	}
	logPackageSkips(namesOf(dir), sel.Files)
	logPackage(sourceName, n, false)
	return n, nil
}

// emitOversizedEntries reports wanted bulk members over the size limit and
// does not read them. They are left out of the range batches.
func emitOversizedEntries(ctx context.Context, entries []cdEntry, out chan<- Member) (int, error) {
	n := 0
	for _, e := range entries {
		name := filepath.ToSlash(e.Name)
		if !wantMember(name) && !wantNestedZip(name) {
			continue
		}
		if e.UncompressedSize <= uint64(maxMemberSize) {
			continue
		}
		added, err := emitTooBig(ctx, name, out)
		if err != nil {
			return n, err
		}
		n += added
	}
	return n, nil
}

func namesOf(dir *zipDirectory) []string {
	names := make([]string, len(dir.Entries))
	for i, e := range dir.Entries {
		names[i] = filepath.ToSlash(e.Name)
	}
	return names
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
		name := filepath.ToSlash(e.Name)
		emitName := name
		if batch.sourceName != "" {
			emitName = batch.sourceName
		}
		if e.UncompressedSize > uint64(maxMemberSize) {
			added, err := emitTooBig(ctx, emitName, out)
			if err != nil {
				return n, err
			}
			n += added
			continue
		}
		content, err := extractMemberFromRange(data, batch.start, e)
		if errors.Is(err, errMemberTooBig) {
			added, err := emitTooBig(ctx, emitName, out)
			if err != nil {
				return n, err
			}
			n += added
			continue
		}
		if err != nil {
			return n, fmt.Errorf("extract %s: %w", name, err)
		}
		if batch.sourceName == "" && wantNestedZip(name) {
			added, err := openZipNamedMember(ctx, name, content, out)
			if err != nil {
				return n, err
			}
			n += added
			continue
		}
		if err := emitMember(ctx, out, Member{Name: emitName, Content: content, OnlyUKFRS: batch.onlyUKFRS}); err != nil {
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
	return packMemberBatchesIf(all, cdOffset, target, maxSpan, gapSplit, nil)
}

// packMemberBatchesIf is packMemberBatches. allow, when non-nil, keeps only
// those normalised entry names (a package's selected reports).
func packMemberBatchesIf(all []cdEntry, cdOffset, target, maxSpan, gapSplit int64, allow map[string]bool) []memberBatch {
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
		if !wantMember(name) && !wantNestedZip(name) {
			continue
		}
		if allow != nil && !allow[normZipName(name)] {
			continue
		}
		if e.UncompressedSize > uint64(maxMemberSize) {
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

	if e.UncompressedSize > uint64(maxMemberSize) {
		return nil, fmt.Errorf("member %s: %w", e.Name, errMemberTooBig)
	}

	var raw []byte
	switch e.Method {
	case methodStore:
		raw = make([]byte, len(comp))
		copy(raw, comp)
	case methodDeflate:
		fr := flate.NewReader(bytes.NewReader(comp))
		var err error
		raw, err = io.ReadAll(io.LimitReader(fr, int64(maxMemberSize)+1))
		_ = fr.Close()
		if err != nil {
			return nil, err
		}
		if int64(len(raw)) > maxMemberSize {
			return nil, fmt.Errorf("member %s: %w", e.Name, errMemberTooBig)
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
