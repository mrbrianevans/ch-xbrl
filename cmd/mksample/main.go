// Command mksample packs sample iXBRL files into a .tar.zst for local testing.
// Includes modern {company}_aa_*.xhtml and bulk Prod*_*.html naming conventions.
//
//	go run ./cmd/mksample -out samples/sample.tar.zst
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrbrianevans/ch-xbrl/internal/archive"
)

func main() {
	dir := flag.String("dir", "samples", "directory of sample iXBRL files")
	out := flag.String("out", "samples/sample.tar.zst", "output tar.zst path")
	flag.Parse()

	entries := map[string]string{}
	err := filepath.WalkDir(*dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		low := strings.ToLower(d.Name())
		if strings.HasSuffix(low, ".tar.zst") || strings.HasSuffix(low, ".zst") {
			return nil
		}
		// .html = bulk Prod* dumps; .xhtml = newer accounts packages
		if !strings.HasSuffix(low, ".xhtml") && !strings.HasSuffix(low, ".html") &&
			!strings.HasSuffix(low, ".htm") && !strings.HasSuffix(low, ".xbrl") &&
			!strings.HasSuffix(low, ".xml") {
			return nil
		}
		// archive member name = basename (flat layout, typical of CH bulk dumps)
		entries[d.Name()] = path
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(entries) == 0 {
		log.Fatalf("no iXBRL files under %s", *dir)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := archive.WriteTarZst(*out, entries); err != nil {
		log.Fatal(err)
	}

	info, _ := os.Stat(*out)
	fmt.Printf("wrote %s (%d members, %d bytes)\n", *out, len(entries), info.Size())
	for name := range entries {
		fmt.Printf("  %s\n", name)
	}
}
