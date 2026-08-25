package archive

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
)

// ZIP signatures and fixed header sizes (PKWARE APPNOTE).
const (
	sigLocalFile        = 0x04034b50
	sigCentralDir       = 0x02014b50
	sigDirectoryEnd     = 0x06054b50
	sigDirectory64Loc   = 0x07064b50
	sigDirectory64End   = 0x06064b50
	localFileHeaderLen  = 30
	centralDirHeaderLen = 46
	directoryEndLen     = 22
	directory64LocLen   = 20
	directory64EndLen   = 56
	zip64ExtraID        = 0x0001
	methodStore         = 0
	methodDeflate       = 8
)

// cdEntry is one central-directory record (subset needed for remote batch reads).
type cdEntry struct {
	Name              string
	Method            uint16
	Flags             uint16
	CRC32             uint32
	CompressedSize    uint64
	UncompressedSize  uint64
	LocalHeaderOffset int64
}

// zipDirectory is the parsed central directory plus layout bounds.
type zipDirectory struct {
	Entries    []cdEntry
	Size       int64 // full archive size
	CDOffset   int64 // start of central directory
	CDSize     uint64
	BaseOffset int64 // non-zero for some prepended-data zips
}

// loadZipDirectoryFromReaderAt parses EOCD (+ Zip64) and the central directory
// using random access. Used for local verification helpers and tests.
func loadZipDirectoryFromReaderAt(ra io.ReaderAt, size int64) (*zipDirectory, error) {
	end, base, err := readDirectoryEnd(ra, size)
	if err != nil {
		return nil, err
	}
	cdOff := base + int64(end.directoryOffset)
	if cdOff < 0 || cdOff >= size {
		return nil, fmt.Errorf("zip: invalid central directory offset %d", cdOff)
	}
	cdSize := int64(end.directorySize)
	if cdSize < 0 || cdOff+cdSize > size {
		return nil, fmt.Errorf("zip: invalid central directory size %d", cdSize)
	}
	buf := make([]byte, cdSize)
	if _, err := ra.ReadAt(buf, cdOff); err != nil && err != io.EOF {
		return nil, err
	}
	entries, err := parseCentralDirectory(buf, base)
	if err != nil {
		return nil, err
	}
	return &zipDirectory{
		Entries:    entries,
		Size:       size,
		CDOffset:   cdOff,
		CDSize:     end.directorySize,
		BaseOffset: base,
	}, nil
}

// loadRemoteZipDirectory fetches the EOCD region and full central directory with
// a small number of HTTP range requests (typically 2–3), independent of member count.
func loadRemoteZipDirectory(ctx context.Context, client *http.Client, url string, size int64) (*zipDirectory, error) {
	// Tail covers EOCD + max comment (64 KiB) + Zip64 locator.
	const maxTail = 65557
	tailLen := int64(maxTail)
	if tailLen > size {
		tailLen = size
	}
	tailStart := size - tailLen
	tail, err := rangeGET(ctx, client, url, tailStart, size-1)
	if err != nil {
		return nil, fmt.Errorf("fetch zip tail: %w", err)
	}

	// Serve tail (+ optional Zip64 EOCD outside tail) via a small region ReaderAt.
	ra := &regionReaderAt{size: size}
	ra.add(tailStart, tail)

	end, base, err := readDirectoryEnd(ra, size)
	if err != nil {
		// Zip64 EOCD may sit before the tail window on huge archives.
		if locOff, locErr := peekDirectory64Locator(tail, tailStart); locErr == nil && locOff >= 0 && locOff < tailStart {
			z64end := locOff + directory64EndLen - 1
			if z64end >= size {
				z64end = size - 1
			}
			chunk, gerr := rangeGET(ctx, client, url, locOff, z64end)
			if gerr != nil {
				return nil, fmt.Errorf("fetch zip64 eocd: %w", gerr)
			}
			ra.add(locOff, chunk)
			end, base, err = readDirectoryEnd(ra, size)
		}
		if err != nil {
			return nil, fmt.Errorf("zip directory end: %w", err)
		}
	}

	cdOff := base + int64(end.directoryOffset)
	cdSize := int64(end.directorySize)
	if cdOff < 0 || cdSize < 0 || cdOff+cdSize > size {
		return nil, fmt.Errorf("zip: central directory bounds %d+%d outside archive size %d", cdOff, cdSize, size)
	}

	// If CD is entirely inside the tail buffer, reuse it; else one Range for the CD.
	var cdBuf []byte
	if cdOff >= tailStart && cdOff+cdSize <= size {
		rel := cdOff - tailStart
		cdBuf = tail[rel : rel+cdSize]
	} else {
		cdBuf, err = rangeGET(ctx, client, url, cdOff, cdOff+cdSize-1)
		if err != nil {
			return nil, fmt.Errorf("fetch central directory: %w", err)
		}
	}

	entries, err := parseCentralDirectory(cdBuf, base)
	if err != nil {
		return nil, err
	}
	return &zipDirectory{
		Entries:    entries,
		Size:       size,
		CDOffset:   cdOff,
		CDSize:     end.directorySize,
		BaseOffset: base,
	}, nil
}

// regionReaderAt serves ReadAt from a set of disjoint prefetched regions.
type regionReaderAt struct {
	size    int64
	regions []struct {
		off  int64
		data []byte
	}
}

func (r *regionReaderAt) add(off int64, data []byte) {
	r.regions = append(r.regions, struct {
		off  int64
		data []byte
	}{off: off, data: data})
}

func (r *regionReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("negative offset")
	}
	if off >= r.size {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) {
		cur := off + int64(n)
		if cur >= r.size {
			if n == 0 {
				return 0, io.EOF
			}
			return n, io.EOF
		}
		chunk, ok := r.find(cur)
		if !ok {
			return n, fmt.Errorf("zip: read at %d not in prefetched regions", cur)
		}
		copied := copy(p[n:], chunk)
		n += copied
		if copied == 0 {
			break
		}
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *regionReaderAt) find(off int64) ([]byte, bool) {
	for _, reg := range r.regions {
		if off >= reg.off && off < reg.off+int64(len(reg.data)) {
			return reg.data[off-reg.off:], true
		}
	}
	return nil, false
}

type directoryEnd struct {
	directoryRecords uint64
	directorySize    uint64
	directoryOffset  uint64
}

func readDirectoryEnd(r io.ReaderAt, size int64) (*directoryEnd, int64, error) {
	var buf []byte
	var directoryEndOffset int64
	found := false
	for i, bLen := range []int64{1024, 65 * 1024} {
		if bLen > size {
			bLen = size
		}
		buf = make([]byte, int(bLen))
		if _, err := r.ReadAt(buf, size-bLen); err != nil && err != io.EOF {
			return nil, 0, err
		}
		if p := findEOCDSignature(buf); p >= 0 {
			buf = buf[p:]
			directoryEndOffset = size - bLen + int64(p)
			found = true
			break
		}
		if i == 1 || bLen == size {
			return nil, 0, fmt.Errorf("zip: EOCD not found")
		}
	}
	if !found || len(buf) < directoryEndLen {
		return nil, 0, fmt.Errorf("zip: EOCD not found")
	}

	b := buf[4:] // skip signature
	d := &directoryEnd{
		directoryRecords: uint64(binary.LittleEndian.Uint16(b[6:8])),
		directorySize:    uint64(binary.LittleEndian.Uint32(b[8:12])),
		directoryOffset:  uint64(binary.LittleEndian.Uint32(b[12:16])),
	}
	commentLen := binary.LittleEndian.Uint16(b[16:18])
	if int(commentLen) > len(b)-18 {
		return nil, 0, fmt.Errorf("zip: invalid EOCD comment length")
	}

	if d.directoryRecords == 0xffff || d.directorySize == 0xffff || d.directoryOffset == 0xffffffff {
		p, err := findDirectory64End(r, directoryEndOffset)
		if err != nil {
			return nil, 0, err
		}
		if p >= 0 {
			if err := readDirectory64End(r, p, d); err != nil {
				return nil, 0, err
			}
			directoryEndOffset = p
		}
	}

	maxInt64 := uint64(1<<63 - 1)
	if d.directorySize > maxInt64 || d.directoryOffset > maxInt64 {
		return nil, 0, fmt.Errorf("zip: directory size/offset too large")
	}

	baseOffset := directoryEndOffset - int64(d.directorySize) - int64(d.directoryOffset)
	if o := baseOffset + int64(d.directoryOffset); o < 0 || o >= size {
		return nil, 0, fmt.Errorf("zip: directory offset out of range")
	}

	// Some archives advertise a non-zero baseOffset incorrectly; if the CD
	// header is valid at absolute directoryOffset, prefer base 0.
	if baseOffset > 0 {
		off := int64(d.directoryOffset)
		hdr := make([]byte, 4)
		if _, err := r.ReadAt(hdr, off); err == nil && binary.LittleEndian.Uint32(hdr) == sigCentralDir {
			baseOffset = 0
		}
	}

	return d, baseOffset, nil
}

func findEOCDSignature(b []byte) int {
	for i := len(b) - directoryEndLen; i >= 0; i-- {
		if b[i] == 'P' && b[i+1] == 'K' && b[i+2] == 0x05 && b[i+3] == 0x06 {
			n := int(b[i+directoryEndLen-2]) | int(b[i+directoryEndLen-1])<<8
			if n+directoryEndLen+i > len(b) {
				return -1
			}
			return i
		}
	}
	return -1
}

func findDirectory64End(r io.ReaderAt, directoryEndOffset int64) (int64, error) {
	locOffset := directoryEndOffset - directory64LocLen
	if locOffset < 0 {
		return -1, nil
	}
	buf := make([]byte, directory64LocLen)
	if _, err := r.ReadAt(buf, locOffset); err != nil {
		return -1, err
	}
	if binary.LittleEndian.Uint32(buf[0:4]) != sigDirectory64Loc {
		return -1, nil
	}
	if binary.LittleEndian.Uint32(buf[4:8]) != 0 {
		return -1, nil
	}
	p := binary.LittleEndian.Uint64(buf[8:16])
	if binary.LittleEndian.Uint32(buf[16:20]) != 1 {
		return -1, nil
	}
	return int64(p), nil
}

func readDirectory64End(r io.ReaderAt, offset int64, d *directoryEnd) error {
	buf := make([]byte, directory64EndLen)
	if _, err := r.ReadAt(buf, offset); err != nil {
		return err
	}
	if binary.LittleEndian.Uint32(buf[0:4]) != sigDirectory64End {
		return fmt.Errorf("zip: invalid zip64 EOCD signature")
	}
	// Layout: sig(4) size(8) ver(2) verNeed(2) disk(4) dirDisk(4)
	// recordsThisDisk(8) records(8) cdSize(8) cdOffset(8)
	d.directoryRecords = binary.LittleEndian.Uint64(buf[32:40])
	d.directorySize = binary.LittleEndian.Uint64(buf[40:48])
	d.directoryOffset = binary.LittleEndian.Uint64(buf[48:56])
	return nil
}

// peekDirectory64Locator looks for a Zip64 locator just before EOCD inside a tail buffer.
func peekDirectory64Locator(tail []byte, tailStart int64) (zip64EOCDOffset int64, err error) {
	p := findEOCDSignature(tail)
	if p < 0 {
		return -1, fmt.Errorf("no eocd")
	}
	directoryEndOffset := tailStart + int64(p)
	locOffset := directoryEndOffset - directory64LocLen
	if locOffset < tailStart {
		return -1, fmt.Errorf("locator before tail")
	}
	rel := int(locOffset - tailStart)
	buf := tail[rel : rel+directory64LocLen]
	if binary.LittleEndian.Uint32(buf[0:4]) != sigDirectory64Loc {
		return -1, nil
	}
	if binary.LittleEndian.Uint32(buf[4:8]) != 0 {
		return -1, nil
	}
	off := int64(binary.LittleEndian.Uint64(buf[8:16]))
	if binary.LittleEndian.Uint32(buf[16:20]) != 1 {
		return -1, nil
	}
	return off, nil
}

func parseCentralDirectory(cd []byte, baseOffset int64) ([]cdEntry, error) {
	var entries []cdEntry
	off := 0
	for off+centralDirHeaderLen <= len(cd) {
		if binary.LittleEndian.Uint32(cd[off:off+4]) != sigCentralDir {
			// Padding / digital signature records can follow; stop at first non-CD.
			break
		}
		e, n, err := parseCentralDirHeader(cd[off:], baseOffset)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
		off += n
	}
	return entries, nil
}

func parseCentralDirHeader(b []byte, baseOffset int64) (cdEntry, int, error) {
	if len(b) < centralDirHeaderLen {
		return cdEntry{}, 0, io.ErrUnexpectedEOF
	}
	method := binary.LittleEndian.Uint16(b[10:12])
	flags := binary.LittleEndian.Uint16(b[8:10])
	crc := binary.LittleEndian.Uint32(b[16:20])
	comp32 := binary.LittleEndian.Uint32(b[20:24])
	uncomp32 := binary.LittleEndian.Uint32(b[24:28])
	nameLen := int(binary.LittleEndian.Uint16(b[28:30]))
	extraLen := int(binary.LittleEndian.Uint16(b[30:32]))
	commentLen := int(binary.LittleEndian.Uint16(b[32:34]))
	headerOff32 := binary.LittleEndian.Uint32(b[42:46])

	total := centralDirHeaderLen + nameLen + extraLen + commentLen
	if len(b) < total {
		return cdEntry{}, 0, io.ErrUnexpectedEOF
	}
	name := string(b[centralDirHeaderLen : centralDirHeaderLen+nameLen])
	extra := b[centralDirHeaderLen+nameLen : centralDirHeaderLen+nameLen+extraLen]

	comp := uint64(comp32)
	uncomp := uint64(uncomp32)
	headerOff := int64(headerOff32)

	needU := uncomp32 == ^uint32(0)
	needC := comp32 == ^uint32(0)
	needH := headerOff32 == ^uint32(0)

	if needU || needC || needH {
		if err := applyZip64Extra(extra, &uncomp, &comp, &headerOff, needU, needC, needH); err != nil {
			return cdEntry{}, 0, err
		}
	}

	return cdEntry{
		Name:              name,
		Method:            method,
		Flags:             flags,
		CRC32:             crc,
		CompressedSize:    comp,
		UncompressedSize:  uncomp,
		LocalHeaderOffset: baseOffset + headerOff,
	}, total, nil
}

func applyZip64Extra(extra []byte, uncomp, comp *uint64, headerOff *int64, needU, needC, needH bool) error {
	for len(extra) >= 4 {
		tag := binary.LittleEndian.Uint16(extra[0:2])
		sz := int(binary.LittleEndian.Uint16(extra[2:4]))
		extra = extra[4:]
		if len(extra) < sz {
			return fmt.Errorf("zip: truncated extra field")
		}
		field := extra[:sz]
		extra = extra[sz:]
		if tag != zip64ExtraID {
			continue
		}
		fb := field
		if needU {
			if len(fb) < 8 {
				return fmt.Errorf("zip: zip64 extra missing uncompressed size")
			}
			*uncomp = binary.LittleEndian.Uint64(fb[0:8])
			fb = fb[8:]
			needU = false
		}
		if needC {
			if len(fb) < 8 {
				return fmt.Errorf("zip: zip64 extra missing compressed size")
			}
			*comp = binary.LittleEndian.Uint64(fb[0:8])
			fb = fb[8:]
			needC = false
		}
		if needH {
			if len(fb) < 8 {
				return fmt.Errorf("zip: zip64 extra missing header offset")
			}
			*headerOff = int64(binary.LittleEndian.Uint64(fb[0:8]))
			needH = false
		}
	}
	if needC || needH {
		return fmt.Errorf("zip: missing zip64 extended information")
	}
	return nil
}
