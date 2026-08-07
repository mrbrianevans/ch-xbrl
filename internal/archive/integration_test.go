//go:build integration

package archive

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// Real Companies House bulk ZIP (range-capable via CloudFront/S3).
//
//	set CH_XBR_INTEGRATION=1
//	go test ./internal/archive/ -tags=integration -run TestIntegrationRemoteCHZip -count=1 -v
const chBulkZipURL = "https://download.companieshouse.gov.uk/Accounts_Bulk_Data-2026-05-09.zip"

func TestIntegrationRemoteCHZip(t *testing.T) {
	if os.Getenv("CH_XBR_INTEGRATION") == "" {
		t.Skip("set CH_XBR_INTEGRATION=1 to hit live Companies House bulk ZIP")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ch := make(chan Member, 4)
	var (
		got int
		mu  sync.Mutex
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for m := range ch {
			if len(m.Content) == 0 {
				t.Errorf("empty member %s", m.Name)
			}
			if !isXBRLName(m.Name) {
				t.Errorf("non-XBRL member %s", m.Name)
			}
			mu.Lock()
			got++
			n := got
			mu.Unlock()
			if n >= 5 {
				// Enough to prove range streaming works; cancel to avoid full ~55 MiB pull.
				cancel()
			}
		}
	}()

	count, err := Stream(ctx, chBulkZipURL, ch)
	wg.Wait()

	mu.Lock()
	n := got
	mu.Unlock()
	if n == 0 {
		t.Fatalf("no members streamed; count=%d err=%v", count, err)
	}
	t.Logf("streamed %d members (stop after ~5); stream count=%d err=%v", n, count, err)
}
