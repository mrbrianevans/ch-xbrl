package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRangeGETRetries5xxThenSucceeds(t *testing.T) {
	oldA, oldB := remoteHTTPAttempts, remoteHTTPBackoff
	remoteHTTPAttempts, remoteHTTPBackoff = 5, 0
	t.Cleanup(func() {
		remoteHTTPAttempts, remoteHTTPBackoff = oldA, oldB
	})

	payload := bytes.Repeat([]byte("abcdefghij"), 100)
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		if i < 3 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		rangeFileServerHandler(payload).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	got, err := rangeGET(context.Background(), srv.Client(), srv.URL+"/blob.bin", 0, 9)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abcdefghij" {
		t.Fatalf("got %q", got)
	}
	if n.Load() != 3 {
		t.Fatalf("requests=%d want 3", n.Load())
	}
}

func TestRangeGETRetries429(t *testing.T) {
	oldA, oldB := remoteHTTPAttempts, remoteHTTPBackoff
	remoteHTTPAttempts, remoteHTTPBackoff = 4, 0
	t.Cleanup(func() {
		remoteHTTPAttempts, remoteHTTPBackoff = oldA, oldB
	})

	payload := []byte("0123456789")
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		rangeFileServerHandler(payload).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	got, err := rangeGET(context.Background(), srv.Client(), srv.URL+"/x", 0, 9)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
}

func TestRangeGETDoesNotRetry404(t *testing.T) {
	oldA := remoteHTTPAttempts
	remoteHTTPAttempts = 6
	t.Cleanup(func() { remoteHTTPAttempts = oldA })

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, err := rangeGET(context.Background(), srv.Client(), srv.URL+"/missing", 0, 10)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *httpError
	if !errors.As(err, &he) || he.StatusCode != 404 {
		t.Fatalf("want 404, got %v", err)
	}
	if n.Load() != 1 {
		t.Fatalf("requests=%d want 1", n.Load())
	}
}

func TestRangeGETDoesNotRetry403(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.Error(w, "no", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	_, err := rangeGET(context.Background(), srv.Client(), srv.URL+"/no", 0, 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("requests=%d want 1", n.Load())
	}
}

func TestRangeGETRetriesShortBody(t *testing.T) {
	oldA, oldB := remoteHTTPAttempts, remoteHTTPBackoff
	remoteHTTPAttempts, remoteHTTPBackoff = 4, 0
	t.Cleanup(func() {
		remoteHTTPAttempts, remoteHTTPBackoff = oldA, oldB
	})

	payload := bytes.Repeat([]byte("x"), 100)
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		w.Header().Set("Content-Range", "bytes 0-99/100")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusPartialContent)
		if i == 1 {
			_, _ = w.Write(payload[:10])
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	got, err := rangeGET(context.Background(), srv.Client(), srv.URL+"/x", 0, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 100 {
		t.Fatalf("len=%d", len(got))
	}
	if n.Load() != 2 {
		t.Fatalf("requests=%d want 2", n.Load())
	}
}

func TestRangeGETExhaustedStillErrors(t *testing.T) {
	oldA, oldB := remoteHTTPAttempts, remoteHTTPBackoff
	remoteHTTPAttempts, remoteHTTPBackoff = 3, 0
	t.Cleanup(func() {
		remoteHTTPAttempts, remoteHTTPBackoff = oldA, oldB
	})

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	_, err := rangeGET(context.Background(), srv.Client(), srv.URL+"/x", 0, 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "retries exhausted") {
		t.Fatalf("err=%v", err)
	}
	if n.Load() != 3 {
		t.Fatalf("requests=%d want 3", n.Load())
	}
}

func TestRangeGETPartialContentLengthHeader(t *testing.T) {
	// sanity: existing range server still works with retries on first-try success
	payload := []byte("hello-range")
	srv := rangeFileServer(t, payload)
	t.Cleanup(srv.Close)
	got, err := rangeGET(context.Background(), srv.Client(), srv.URL+"/b", 0, int64(len(payload)-1))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q", got)
	}
}

func TestOpenHTTPStreamRetries5xx(t *testing.T) {
	oldA, oldB := remoteHTTPAttempts, remoteHTTPBackoff
	remoteHTTPAttempts, remoteHTTPBackoff = 5, 0
	t.Cleanup(func() {
		remoteHTTPAttempts, remoteHTTPBackoff = oldA, oldB
	})

	body := []byte("<?xml version=\"1.0\"?><html/>")
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			http.Error(w, "blip", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/xhtml+xml")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	rc, hdr, err := openHTTPStream(context.Background(), srv.URL+"/document?format=xhtml")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body %q", got)
	}
	if hdr.Get("Content-Type") != "application/xhtml+xml" {
		t.Fatalf("Content-Type %q", hdr.Get("Content-Type"))
	}
	if n.Load() != 3 {
		t.Fatalf("requests=%d want 3", n.Load())
	}
}

func TestOpenHTTPStreamNoRetry404(t *testing.T) {
	oldA := remoteHTTPAttempts
	remoteHTTPAttempts = 6
	t.Cleanup(func() { remoteHTTPAttempts = oldA })

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, _, err := openHTTPStream(context.Background(), srv.URL+"/missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if n.Load() != 1 {
		t.Fatalf("requests=%d want 1", n.Load())
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("err=%v", err)
	}
}

func TestRemoteSizeHEADRetries503(t *testing.T) {
	oldA, oldB := remoteHTTPAttempts, remoteHTTPBackoff
	remoteHTTPAttempts, remoteHTTPBackoff = 5, 0
	t.Cleanup(func() {
		remoteHTTPAttempts, remoteHTTPBackoff = oldA, oldB
	})

	payload := bytes.Repeat([]byte("z"), 2048)
	var heads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			if heads.Add(1) < 3 {
				http.Error(w, "wait", http.StatusServiceUnavailable)
				return
			}
		}
		rangeFileServerHandler(payload).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	n, err := remoteSize(context.Background(), srv.Client(), srv.URL+"/obj")
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("size %d want %d", n, len(payload))
	}
	if heads.Load() != 3 {
		t.Fatalf("HEAD count=%d want 3", heads.Load())
	}
}

func TestRemoteSizeHEADNoRetry404FallsBackToRange(t *testing.T) {
	oldA := remoteHTTPAttempts
	remoteHTTPAttempts = 6
	t.Cleanup(func() { remoteHTTPAttempts = oldA })

	payload := bytes.Repeat([]byte("z"), 512)
	var heads, ranges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			heads.Add(1)
			http.NotFound(w, r)
			return
		}
		ranges.Add(1)
		rangeFileServerHandler(payload).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	n, err := remoteSize(context.Background(), srv.Client(), srv.URL+"/obj")
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("size %d", n)
	}
	if heads.Load() != 1 {
		t.Fatalf("HEAD retries on 404: %d", heads.Load())
	}
	if ranges.Load() < 1 {
		t.Fatal("expected range size probe")
	}
}
