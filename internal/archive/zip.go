package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// streamZip opens a local or remote ZIP and yields iXBRL/XBRL members.
//
// Local: sequential archive/zip over a file ReaderAt.
// Remote: central directory once, then parallel large HTTP Range batches
// (CloudFront/S3) — not one request per ReadAt/member.
func streamZip(ctx context.Context, source string, out chan<- Member) (int, error) {
	if isRemote(source) {
		return streamZipRemote(ctx, source, out)
	}
	return streamZipLocal(ctx, source, out)
}

func streamZipLocal(ctx context.Context, source string, out chan<- Member) (int, error) {
	ra, size, closer, err := openReaderAt(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("open zip: %w", err)
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}

	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return 0, fmt.Errorf("zip: %w", err)
	}

	if sel := selectPackage(zipNames(zr)); sel.Kind != packageBulk {
		return emitPackage(ctx, instanceName(source), zr, sel, false, out)
	}

	n := 0
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		nested := wantNestedZip(name)
		if !nested && !wantMember(name) {
			continue
		}
		if f.UncompressedSize64 > uint64(maxMemberSize) {
			added, err := emitTooBig(ctx, name, out)
			if err != nil {
				return n, err
			}
			n += added
			continue
		}
		content, err := readZipFile(f)
		if errors.Is(err, errMemberTooBig) {
			added, emitErr := emitTooBig(ctx, name, out)
			if emitErr != nil {
				return n, emitErr
			}
			n += added
			continue
		}
		if err != nil {
			return n, err
		}
		if nested {
			added, err := openZipNamedMember(ctx, name, content, out)
			if err != nil {
				return n, err
			}
			n += added
			continue
		}
		if err := emit(ctx, out, name, content); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// errMemberTooBig is a per-file size failure. Callers emit it as Member.Err.
var errMemberTooBig = errors.New("exceeds size limit")

// errNoReports is a package that contained no selected report.
var errNoReports = errors.New("no reports")

// readZipFile reads one zip member, enforcing maxMemberSize.
func readZipFile(f *zip.File) ([]byte, error) {
	name := filepath.ToSlash(f.Name)
	if f.UncompressedSize64 > uint64(maxMemberSize) {
		return nil, fmt.Errorf("member %s: %w", name, errMemberTooBig)
	}
	rc, err := f.Open()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("open member %s: %w", name, err)
	}
	defer func() { _ = rc.Close() }()
	content, err := io.ReadAll(io.LimitReader(rc, maxMemberSize+1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if int64(len(content)) > maxMemberSize {
		return nil, fmt.Errorf("member %s: %w", name, errMemberTooBig)
	}
	return content, nil
}

func emitTooBig(ctx context.Context, name string, out chan<- Member) (int, error) {
	log.Printf("member %s exceeds size limit", name)
	if err := emitMember(ctx, out, Member{Name: name, Err: errMemberTooBig}); err != nil {
		return 0, err
	}
	return 1, nil
}

func emitNoReports(ctx context.Context, name string, out chan<- Member) (int, error) {
	log.Printf("package %s: no reports", name)
	if err := emitMember(ctx, out, Member{Name: name, Err: errNoReports}); err != nil {
		return 0, err
	}
	return 1, nil
}

// errBadNestedZip marks zip bytes that are not an archive. openZipNamedMember
// turns it into a member error. It is not returned to Stream.
var errBadNestedZip = errors.New("bad nested zip")

// openZipNamedMember classifies a member whose name ends in .zip.
// Zip magic is expanded one level. XML/XHTML/iXBRL is one instance: Companies
// House has published iXBRL under a .zip name, and that must be parsed, not
// rejected as a corrupt archive. Anything else (an attachment placeholder,
// empty bytes, or zip magic that is not a valid zip) is a member error: the
// stream continues and the caller counts files_err. source_file for an
// instance is the member name. The positional input is not classified here;
// a path ending in .zip is still an archive.
func openZipNamedMember(ctx context.Context, name string, content []byte, out chan<- Member) (int, error) {
	if isZipMagic(content) {
		n, err := expandNestedZip(ctx, name, content, out)
		if errors.Is(err, errBadNestedZip) {
			return emitBadZipMember(ctx, name, out)
		}
		return n, err
	}
	if isXMLMagic(content) {
		if err := emit(ctx, out, name, content); err != nil {
			return 0, err
		}
		log.Printf("instance named .zip: %s", name)
		return 1, nil
	}
	return emitBadZipMember(ctx, name, out)
}

// emitBadZipMember records one .zip-named member that is not a zip archive
// and not an instance. The log line names it. Member.Err is the per-member
// failure; Stream itself succeeds so later members are still read.
func emitBadZipMember(ctx context.Context, name string, out chan<- Member) (int, error) {
	log.Printf("nested zip %s: %v", name, zip.ErrFormat)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case out <- Member{Name: name, Err: zip.ErrFormat}:
		return 1, nil
	}
}

// expandNestedZip opens one already-inflated zip member. A report package or
// a Companies House package is one filing: source_file is the wrapper name,
// and a package with no selected report is a member error. A nested zip that
// is neither keeps the accounts-directory filter. Inner zips are not opened.
// Zip magic that is not a valid zip returns errBadNestedZip so the caller can
// count a member error. Other failures stay stream errors.
func expandNestedZip(ctx context.Context, name string, content []byte, out chan<- Member) (int, error) {
	if int64(len(content)) > maxMemberSize {
		return emitTooBig(ctx, name, out)
	}
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		if errors.Is(err, zip.ErrFormat) {
			return 0, errBadNestedZip
		}
		return 0, fmt.Errorf("nested zip %s: %w", name, err)
	}
	sel := selectPackage(zipNames(zr))
	if sel.Kind == packageBulk {
		return expandAccountsOnly(ctx, name, zr, out)
	}
	return emitPackage(ctx, name, zr, sel, true, out)
}

// emitPackage emits the selected reports of a package as one filing.
// sourceName is the bulk member name, or the positional input basename.
// nested selects the log line used by the existing nested-zip tests.
func emitPackage(ctx context.Context, sourceName string, zr *zip.Reader, sel packageSelection, nested bool, out chan<- Member) (int, error) {
	if len(sel.Files) == 0 {
		return emitNoReports(ctx, sourceName, out)
	}
	files := zipFilesByName(zr)
	for _, p := range sel.Files {
		f := files[p]
		if f != nil && f.UncompressedSize64 > uint64(maxMemberSize) {
			return emitTooBig(ctx, sourceName, out)
		}
	}
	logPackageSkips(zipNames(zr), sel.Files)
	n := 0
	for _, p := range sel.Files {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		f := files[p]
		if f == nil {
			continue
		}
		body, err := readZipFile(f)
		if errors.Is(err, errMemberTooBig) {
			return emitTooBig(ctx, sourceName, out)
		}
		if err != nil {
			return n, err
		}
		if err := emitMember(ctx, out, Member{Name: sourceName, Content: body, OnlyUKFRS: sel.UKFRS}); err != nil {
			return n, err
		}
		n++
	}
	if n == 0 {
		return emitNoReports(ctx, sourceName, out)
	}
	logPackage(sourceName, n, nested)
	return n, nil
}

func logPackage(sourceName string, n int, nested bool) {
	if nested {
		log.Printf("nested zip: %s (%d members)", sourceName, n)
	} else {
		log.Printf("package: %s (%d members)", sourceName, n)
	}
}

func logPackageSkips(names, selected []string) {
	zips, skipped := packageSkipNames(names, selected)
	for _, inner := range zips {
		log.Printf("skip nested zip: %s", inner)
	}
	for _, inner := range skipped {
		log.Printf("skip nested member: %s", inner)
	}
}

// expandAccountsOnly is the filter for a nested zip that is not a package.
// Only iXBRL/XBRL members under an accounts directory are emitted.
func expandAccountsOnly(ctx context.Context, name string, zr *zip.Reader, out chan<- Member) (int, error) {
	n := 0
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if f.FileInfo().IsDir() {
			continue
		}
		inner := filepath.ToSlash(f.Name)
		if !memberNameOK(inner) {
			continue
		}
		if isZipName(inner) {
			log.Printf("skip nested zip: %s", inner)
			continue
		}
		if !isXBRLName(inner) {
			continue
		}
		if !hasPathSegment(inner, "accounts") {
			log.Printf("skip nested member: %s", inner)
			continue
		}
		if f.UncompressedSize64 > uint64(maxMemberSize) {
			return emitTooBig(ctx, name, out)
		}
		body, err := readZipFile(f)
		if errors.Is(err, errMemberTooBig) {
			return emitTooBig(ctx, name, out)
		}
		if err != nil {
			return n, err
		}
		if err := emit(ctx, out, name, body); err != nil {
			return n, err
		}
		n++
	}
	log.Printf("nested zip: %s (%d members)", name, n)
	return n, nil
}

func zipNames(zr *zip.Reader) []string {
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, filepath.ToSlash(f.Name))
	}
	return names
}

func zipFilesByName(zr *zip.Reader) map[string]*zip.File {
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		key := normZipName(f.Name)
		if key == "" {
			continue
		}
		files[key] = f
	}
	return files
}

func normZipName(name string) string {
	got := normaliseZipNames([]string{name})
	if len(got) == 0 {
		return ""
	}
	return got[0]
}

// WriteZip packs files into a .zip archive at dest.
// entries maps archive member name → local filesystem path.
// Intended for tests and local sample packs (not the ch-xbrl hot path).
func WriteZip(dest string, entries map[string]string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	defer func() { _ = zw.Close() }()

	// Stable order helps tests that care about layout.
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	// sort.Strings would need import; range order is fine for WriteZip.
	for _, name := range names {
		path := entries[name]
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.Method = zip.Deflate
		if hdr.Modified.IsZero() {
			hdr.Modified = time.Now()
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return zw.Close()
}
