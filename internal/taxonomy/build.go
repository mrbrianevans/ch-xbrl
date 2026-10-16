package taxonomy

import (
	"context"
	"fmt"
	"net/http"
)

// Build downloads each source, scans its schemas, and returns concepts
// sorted by namespace then local name. A checksum mismatch, a parse
// error, or an attribute conflict fails the build.
func Build(ctx context.Context, sources []Source, cacheDir string, client *http.Client, logf func(string, ...any)) ([]Concept, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("no taxonomy sources")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	all := map[string]placed{}
	for _, src := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		logf("load %s", src.ID)
		path, err := Fetch(ctx, client, src, cacheDir)
		if err != nil {
			return nil, err
		}
		got, err := ScanZip(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src.ID, err)
		}
		for k, p := range got {
			p.where = src.ID + " (" + p.where + ")"
			prev, ok := all[k]
			if !ok {
				all[k] = p
				continue
			}
			if prev.c != p.c {
				return nil, &ConflictError{
					Concept:       p.c,
					Existing:      prev.c,
					Where:         p.where,
					ExistingWhere: prev.where,
				}
			}
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no concepts")
	}
	return sortedConcepts(all), nil
}
