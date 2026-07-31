package main

import (
	"archive/zip"
	"io"
	"log"
	"path"
	"strings"
)

// scanZipFile walks XSDs inside a taxonomy package ZIP and extracts concepts.
func scanZipFile(zipPath string, concepts map[string]Concept) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if !strings.HasSuffix(strings.ToLower(name), ".xsd") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			log.Printf("zip open %s: %v", name, err)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, 32<<20))
		rc.Close()
		if err != nil {
			log.Printf("zip read %s: %v", name, err)
			continue
		}
		// Parse only this file; network includes are skipped (client nil → fetch fails quietly).
		if err := walkSchema(nil, data, "zip://"+name, concepts, map[string]bool{"zip://" + name: true}, 0); err != nil {
			log.Printf("zip parse %s: %v", name, err)
		}
	}
	return nil
}
