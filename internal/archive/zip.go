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
	"strings"
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
		content, err := readZipFile(f)
		if err != nil {
			return n, err
		}
		if nested {
			added, err := expandNestedZip(ctx, name, content, out)
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

// readZipFile reads one zip member, enforcing maxMemberSize.
func readZipFile(f *zip.File) ([]byte, error) {
	name := filepath.ToSlash(f.Name)
	if f.UncompressedSize64 > maxMemberSize {
		return nil, fmt.Errorf("member %s exceeds size limit", name)
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
	if len(content) > maxMemberSize {
		return nil, fmt.Errorf("member %s exceeds size limit", name)
	}
	return content, nil
}

// expandNestedZip opens one already-inflated zip member and emits its iXBRL
// members. Inner members that are themselves zip files are logged and skipped.
// source_file is a flat name built from the wrapper (company number and date)
// plus the inner document role. The 50 MiB cap applies to the wrapper and
// each inner instance.
func expandNestedZip(ctx context.Context, name string, content []byte, out chan<- Member) (int, error) {
	if int64(len(content)) > maxMemberSize {
		return 0, fmt.Errorf("member %s exceeds size limit", name)
	}
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return 0, fmt.Errorf("nested zip %s: %w", name, err)
	}
	n := 0
	used := map[string]struct{}{}
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
		body, err := readZipFile(f)
		if err != nil {
			return n, err
		}
		if err := emit(ctx, out, nestedInstanceName(name, inner, used), body); err != nil {
			return n, err
		}
		n++
	}
	log.Printf("nested zip: %s (%d members)", name, n)
	return n, nil
}

// nestedInstanceName builds a flat source_file for one file inside a nested zip.
//
// Bulk members look like Prod223_4320_00068622_20260331.html. A CIC wrapper is
// Prod223_4320_05016384_20251231_CIC.zip and the files inside it are not:
// CIC-05016384/accounts/financialStatement.xhtml has the company number only
// as a directory, and no period date. The date lives on the wrapper.
//
// The emitted name keeps the wrapper stem (run, company number, date), drops
// _CIC, and adds the inner folder that says which document it is:
//
//	Prod223_4320_05016384_20251231_accounts.xhtml
//	Prod223_4320_05016384_20251231_cic34.xhtml
//
// The inner extension is kept. A second file in the same folder gets the
// inner basename as well, so the two rows cannot share a source_file.
// Opening the CIC zip itself as the positional input still uses the inner
// member path: that run has no wrapper name to copy.
func nestedInstanceName(wrapper, inner string, used map[string]struct{}) string {
	stem := cicWrapperStem(wrapper)
	inner = filepath.ToSlash(inner)
	base := filepath.Base(inner)
	ext := filepath.Ext(base)
	leaf := strings.TrimSuffix(base, ext)
	role := cicInnerRole(inner)

	try := []string{joinNestedName(stem, role, "", ext)}
	if leaf != "" && !strings.EqualFold(sanitizeNameToken(leaf), sanitizeNameToken(role)) {
		try = append(try, joinNestedName(stem, role, leaf, ext))
	}
	for _, cand := range try {
		if claimName(used, cand) {
			return cand
		}
	}
	baseName := joinNestedName(stem, role, leaf, ext)
	for i := 2; i < 10000; i++ {
		cand := withNumericSuffix(baseName, i)
		if claimName(used, cand) {
			return cand
		}
	}
	return inner
}

func claimName(used map[string]struct{}, name string) bool {
	if name == "" {
		return false
	}
	if _, ok := used[name]; ok {
		return false
	}
	used[name] = struct{}{}
	return true
}

func withNumericSuffix(name string, n int) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%d%s", stem, n, ext)
}

func joinNestedName(stem, role, leaf, ext string) string {
	var parts []string
	if stem != "" {
		parts = append(parts, stem)
	}
	if tok := sanitizeNameToken(role); tok != "" {
		parts = append(parts, tok)
	}
	if tok := sanitizeNameToken(leaf); tok != "" {
		parts = append(parts, tok)
	}
	if len(parts) == 0 {
		parts = append(parts, "instance")
	}
	return strings.Join(parts, "_") + ext
}

// cicWrapperStem is the wrapper basename without .zip and without a trailing _CIC.
func cicWrapperStem(name string) string {
	base := filepath.Base(filepath.ToSlash(name))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	const suf = "_cic"
	if len(stem) >= len(suf) && strings.EqualFold(stem[len(stem)-len(suf):], suf) {
		stem = stem[:len(stem)-len(suf)]
	}
	return stem
}

// cicInnerRole is the folder that distinguishes the accounts document from
// the CIC34 report. The CIC-<number> directory is not a role.
func cicInnerRole(inner string) string {
	dir := filepath.Dir(filepath.ToSlash(inner))
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	parent := filepath.Base(dir)
	if parent == "." || parent == "/" || isCICCompanyDir(parent) {
		return ""
	}
	return parent
}

func isCICCompanyDir(name string) bool {
	rest, ok := strings.CutPrefix(strings.ToLower(name), "cic-")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func sanitizeNameToken(s string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		ok := (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-' || r == '_'
		if !ok {
			if prevUnderscore {
				continue
			}
			b.WriteByte('_')
			prevUnderscore = true
			continue
		}
		prevUnderscore = r == '_'
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "_")
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
