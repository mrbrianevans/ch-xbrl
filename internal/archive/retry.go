package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Remote HTTP retry knobs. Not part of the CLI contract; tune without a major.
var (
	remoteHTTPAttempts   = 6
	remoteHTTPBackoff    = 100 * time.Millisecond
	remoteHTTPBackoffCap = 5 * time.Second
)

// errShortRange marks a Range response shorter than requested (retryable).
var errShortRange = errors.New("short range body")

// httpError is an HTTP status from a remote store (S3/R2/CloudFront).
type httpError struct {
	StatusCode int
	Status     string
	URL        string
	msg        string
}

func (e *httpError) Error() string {
	if e != nil && e.msg != "" {
		return e.msg
	}
	if e == nil {
		return "HTTP error"
	}
	return fmt.Sprintf("HTTP %s for %s", e.Status, e.URL)
}

func httpErrorf(code int, status, rawURL, format string, args ...any) *httpError {
	return &httpError{StatusCode: code, Status: status, URL: rawURL, msg: fmt.Sprintf(format, args...)}
}

func isRetryableHTTP(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var he *httpError
	if errors.As(err, &he) && he != nil {
		switch he.StatusCode {
		case http.StatusForbidden, http.StatusNotFound:
			return false
		case http.StatusTooManyRequests:
			return true
		}
		return he.StatusCode >= 500 && he.StatusCode <= 599
	}
	if errors.Is(err, errShortRange) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return true
		}
		if ue.Err != nil && ue.Err != err {
			return isRetryableHTTP(ue.Err)
		}
	}
	return false
}

func retryHTTP(ctx context.Context, op func() error) error {
	attempts := remoteHTTPAttempts
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		last = op()
		if last == nil {
			return nil
		}
		if !isRetryableHTTP(last) {
			return last
		}
		if i == attempts-1 {
			break
		}
		if err := sleepCtx(ctx, remoteBackoff(i)); err != nil {
			return err
		}
	}
	return fmt.Errorf("retries exhausted (%d attempts): %w", attempts, last)
}

func remoteBackoff(attempt int) time.Duration {
	wait := remoteHTTPBackoff
	if attempt > 0 && wait > 0 {
		shift := attempt
		if shift > 16 {
			shift = 16
		}
		wait = wait << shift
	}
	if wait > remoteHTTPBackoffCap {
		wait = remoteHTTPBackoffCap
	}
	if wait <= 0 {
		return 0
	}
	// Full jitter: uniform in [0, wait].
	return time.Duration(rand.Int64N(int64(wait) + 1))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<20))
	_ = resp.Body.Close()
}
