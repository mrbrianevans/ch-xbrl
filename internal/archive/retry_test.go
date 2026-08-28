package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsRetryableHTTP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", want: false},
		{name: "429", err: &httpError{StatusCode: 429, Status: "429 Too Many Requests"}, want: true},
		{name: "500", err: &httpError{StatusCode: 500, Status: "500 Internal Server Error"}, want: true},
		{name: "502", err: &httpError{StatusCode: 502, Status: "502 Bad Gateway"}, want: true},
		{name: "503", err: &httpError{StatusCode: 503, Status: "503 Service Unavailable"}, want: true},
		{name: "504", err: &httpError{StatusCode: 504, Status: "504 Gateway Timeout"}, want: true},
		{name: "403", err: &httpError{StatusCode: 403, Status: "403 Forbidden"}, want: false},
		{name: "404", err: &httpError{StatusCode: 404, Status: "404 Not Found"}, want: false},
		{name: "400", err: &httpError{StatusCode: 400, Status: "400 Bad Request"}, want: false},
		{name: "416", err: &httpError{StatusCode: 416, Status: "416 Range Not Satisfiable"}, want: false},
		{name: "short range", err: fmt.Errorf("short: %w", errShortRange), want: true},
		{name: "unexpected EOF", err: fmt.Errorf("read: %w", io.ErrUnexpectedEOF), want: true},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "deadline", err: context.DeadlineExceeded, want: false},
		{name: "wrapped 404", err: fmt.Errorf("probe: %w", &httpError{StatusCode: 404, Status: "404"}), want: false},
		{name: "wrapped 503", err: fmt.Errorf("probe: %w", &httpError{StatusCode: 503, Status: "503"}), want: true},
		{name: "url timeout", err: &url.Error{Op: "Get", URL: "http://x", Err: timeoutError{}}, want: true},
		{name: "plain", err: errors.New("nope"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isRetryableHTTP(tc.err); got != tc.want {
				t.Fatalf("isRetryableHTTP(%v)=%v want %v", tc.err, got, tc.want)
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

func TestRetryHTTPEventuallySucceeds(t *testing.T) {
	oldA, oldB := remoteHTTPAttempts, remoteHTTPBackoff
	remoteHTTPAttempts, remoteHTTPBackoff = 5, 0
	t.Cleanup(func() {
		remoteHTTPAttempts, remoteHTTPBackoff = oldA, oldB
	})

	var n atomic.Int32
	err := retryHTTP(context.Background(), func() error {
		if n.Add(1) < 3 {
			return &httpError{StatusCode: 503, Status: "503"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 3 {
		t.Fatalf("attempts=%d want 3", n.Load())
	}
}

func TestRetryHTTPDoesNotRetry404(t *testing.T) {
	oldA := remoteHTTPAttempts
	remoteHTTPAttempts = 6
	t.Cleanup(func() { remoteHTTPAttempts = oldA })

	var n atomic.Int32
	err := retryHTTP(context.Background(), func() error {
		n.Add(1)
		return &httpError{StatusCode: 404, Status: "404 Not Found", URL: "http://x"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("attempts=%d want 1", n.Load())
	}
}

func TestRetryHTTPDoesNotRetry403(t *testing.T) {
	var n atomic.Int32
	err := retryHTTP(context.Background(), func() error {
		n.Add(1)
		return &httpError{StatusCode: 403, Status: "403 Forbidden", URL: "http://x"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("attempts=%d want 1", n.Load())
	}
}

func TestRetryHTTPExhausted(t *testing.T) {
	oldA, oldB := remoteHTTPAttempts, remoteHTTPBackoff
	remoteHTTPAttempts, remoteHTTPBackoff = 3, 0
	t.Cleanup(func() {
		remoteHTTPAttempts, remoteHTTPBackoff = oldA, oldB
	})

	var n atomic.Int32
	err := retryHTTP(context.Background(), func() error {
		n.Add(1)
		return &httpError{StatusCode: 429, Status: "429"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 3 {
		t.Fatalf("attempts=%d want 3", n.Load())
	}
	if got := err.Error(); !strings.Contains(got, "retries exhausted") || !strings.Contains(got, "429") {
		t.Fatalf("error %q", got)
	}
	var he *httpError
	if !errors.As(err, &he) || he.StatusCode != 429 {
		t.Fatalf("want wrapped 429, got %v", err)
	}
}

func TestRetryHTTPHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := retryHTTP(ctx, func() error {
		t.Fatal("op should not run")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestRemoteBackoffCappedAndNonNegative(t *testing.T) {
	oldB, oldC := remoteHTTPBackoff, remoteHTTPBackoffCap
	remoteHTTPBackoff = time.Second
	remoteHTTPBackoffCap = 50 * time.Millisecond
	t.Cleanup(func() {
		remoteHTTPBackoff, remoteHTTPBackoffCap = oldB, oldC
	})
	for i := 0; i < 8; i++ {
		d := remoteBackoff(i)
		if d < 0 || d > remoteHTTPBackoffCap {
			t.Fatalf("attempt %d: backoff %s out of range", i, d)
		}
	}
}
