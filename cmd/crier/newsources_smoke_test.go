//go:build smoke

// eyeball check for the netflix + nvidia sources: fetch them live,
// run every posting through the REAL filter, and print keep/drop so a
// human can confirm we aren't dropping good new-grad swe roles.
// behind the smoke tag like the rest, run with `make smoke`

package main

import (
	"context"
	"testing"
	"time"

	"github.com/Lavanic/crier/internal/config"
	"github.com/Lavanic/crier/internal/filter"
	"github.com/Lavanic/crier/internal/sources"
)

func TestSmokeNewSourcesThroughFilter(t *testing.T) {
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	f, err := filter.New(filter.Config{
		Include:                cfg.Filter.Include,
		ExcludeKeywords:        cfg.Filter.ExcludeKeywords,
		ExcludePatterns:        cfg.Filter.ExcludePatterns,
		ExcludeLocations:       cfg.Filter.ExcludeLocations,
		ExcludeCategories:      cfg.Filter.ExcludeCategories,
		CategoryRescueKeywords: cfg.Filter.CategoryRescueKeywords,
	})
	if err != nil {
		t.Fatalf("build filter: %v", err)
	}

	client := sources.NewHTTPClient()
	var srcs []sources.Source
	for _, feed := range cfg.Sources.Netflix {
		srcs = append(srcs, sources.NewNetflixJobs(client, feed.Name, feed.URL, 0))
	}
	for _, w := range cfg.Sources.Workday {
		srcs = append(srcs, sources.NewWorkday(client, w.Name, w.Company, w.URL, w.Search, 0))
	}

	for _, s := range srcs {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		jobs, err := s.Fetch(ctx)
		cancel()
		if err != nil {
			t.Errorf("%s fetch failed: %v", s.Name(), err)
			continue
		}
		kept := 0
		t.Logf("=== %s: %d jobs ===", s.Name(), len(jobs))
		for _, j := range jobs {
			mark := "drop"
			if f.Match(j) {
				mark = "KEEP"
				kept++
			}
			t.Logf("  [%s] %s | %s", mark, j.Title, j.Location)
		}
		t.Logf("=== %s: %d kept of %d ===", s.Name(), kept, len(jobs))
	}
}
