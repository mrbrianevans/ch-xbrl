package taxonomy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxZipBytes = 64 << 20

// Fetch returns a local zip for src. An http(s) URL is downloaded into
// cacheDir and kept only when the bytes match src.SHA256. A local path
// is used in place when its hash matches.
func Fetch(ctx context.Context, client *http.Client, src Source, cacheDir string) (string, error) {
	sum, err := normalizeSHA(src.SHA256)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src.ID, err)
	}
	if err := validateID(src.ID); err != nil {
		return "", err
	}
	if local, ok := localPath(src.URL); ok {
		if err := hashFileOK(local, sum); err != nil {
			return "", fmt.Errorf("%s: %w", src.ID, err)
		}
		return local, nil
	}
	if !strings.HasPrefix(src.URL, "https://") && !strings.HasPrefix(src.URL, "http://") {
		return "", fmt.Errorf("%s: unsupported URL %q", src.ID, src.URL)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(cacheDir, src.ID+".zip")
	if err := hashFileOK(dest, sum); err == nil {
		return dest, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	body, err := download(ctx, client, src.URL)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src.ID, err)
	}
	got := sha256.Sum256(body)
	if hex.EncodeToString(got[:]) != sum {
		return "", fmt.Errorf("%s: sha256 mismatch for %s", src.ID, src.URL)
	}
	tmp := dest + ".partial"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dest, nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	var last error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt-1) * 500 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ch-xbrl-taxonomy")
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			last = fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxZipBytes+1))
		resp.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if len(body) > maxZipBytes {
			return nil, fmt.Errorf("%s: larger than %d bytes", url, maxZipBytes)
		}
		return body, nil
	}
	return nil, last
}

func localPath(url string) (string, bool) {
	switch {
	case strings.HasPrefix(url, "file://"):
		return strings.TrimPrefix(url, "file://"), true
	case strings.HasPrefix(url, "/") || strings.HasPrefix(url, "./") || strings.HasPrefix(url, "../"):
		return url, true
	default:
		return "", false
	}
}

func hashFileOK(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("sha256 mismatch for %s", path)
	}
	return nil
}

func normalizeSHA(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 64 {
		return "", fmt.Errorf("sha256 must be 64 hex characters")
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", fmt.Errorf("sha256 must be hex")
		}
	}
	return s, nil
}

func validateID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid source id %q", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("invalid source id %q", id)
		}
	}
	return nil
}
